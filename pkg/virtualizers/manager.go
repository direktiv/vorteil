package virtualizers

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"text/template"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
)

// ManagerArgs are the arguments required to create Virtualizer Manager
type ManagerArgs struct {
	Logger          func(format string, v ...interface{})
	DatabaseAddress string
	FirecrackerPath string // path to folder for vmlinux binaries
	Passphrase      string
	VMDrive         string // path to store vms will be /tmp if not provided
	// Subserver       *graph.Graph
}

// Manager is main object which the daemon will interact with
type Manager struct {
	log             func(format string, v ...interface{})
	database        *sql.DB
	firecrackerPath string
	databaseAddr    string
	passphrase      string
	// subserver       *graph.Graph
	vmdrive string
}

// virtualizerTable a generic json object which we will marshal and store under one field for the database
var virtualizerTable = map[string]string{
	"Table": "virtualizers",
	"Name":  "name",
	"Type":  "type",
	"Data":  "data",
}

type virtualizerTuple struct {
	Name string
	Type string
	Data []byte
}

type VState string

const (
	_        VState = "state"
	Ready           = "ready"
	Alive           = "alive"
	Broken          = "broken"
	Deleted         = "deleted"
	Changing        = "changing"
)

// supportedVirtualizers is an array of hypervisors we currently support.
var supportedVirtualizers = []string{"qemu", "virtualbox", "vmware", "hyperv", "firecracker"}

// default values set for linux
var qemu = "/usr/bin"
var vbox = "/usr/bin"
var vmware = "/usr/bin"
var firecracker = "/usr/bin"

// Powershell is the path to exec from
var Powershell string

// init checks what operating system we're currently running on
// that way we can setup the paths and find default installs of the hypervisors.
func init() {
	var err error
	switch runtime.GOOS {
	case "windows":
		qemu, err = exec.LookPath("qemu-system-x86_64.exe")
		if err != nil {
			// try default path instead
			qemu = "C:\\Program Files\\qemu"
		} else {
			qemu = filepath.Dir(qemu)
		}
		vbox, err = exec.LookPath("VBoxManage.exe")
		if err != nil {
			// try default installation path
			vbox = "C:\\Program Files\\Oracle\\VirtualBox"
		} else {
			vbox = filepath.Dir(vbox)
		}
		vmware, err = exec.LookPath("vmrun.exe")
		if err != nil {
			// try default path
			vmware = "C:\\Program Files (x86)\\VMware\\VMware Workstation"
		} else {
			vmware = filepath.Dir(vmware)
		}
		Powershell, _ = exec.LookPath("powershell.exe")

	case "darwin":
		qemu, err = exec.LookPath("qemu-system-x86_64")
		if err != nil {
			// try default path instead
			qemu = "/opt/local/bin:/usr/local/bin"
		} else {
			qemu = filepath.Dir(qemu)
		}
		vbox, err = exec.LookPath("VBoxManage")
		if err != nil {
			// try default installation path
			vbox = "/Applications/VirtualBox.app/Contents/MacOS"
		} else {
			vbox = filepath.Dir(vbox)
		}
		vmware, err = exec.LookPath("vmrun")
		if err != nil {
			// try default path
			vmware = "/Applications/VMware Fusion.app/Contents/Library"
		} else {
			vmware = filepath.Dir(vmware)
		}
	}
}

// // AppendGraph attaches the graph used to update subscriptions to the manager after it was finalized
// func (mgr *Manager) AppendGraph(graph *graph.Graph) {
// 	mgr.subserver = graph
// }

func (mgr *Manager) createDB() error {
	var err error

	mgr.database, err = sql.Open("sqlite3", mgr.databaseAddr)
	if err != nil {
		return err
	}
	mgr.log("Connected to database: %s.", mgr.databaseAddr)

	_, err = mgr.database.Exec("PRAGMA foreign_keys = ON")
	if err != nil {
		return err
	}
	mgr.log("Enabled database foreign key support.")

	_, err = mgr.database.Exec("PRAGMA secure_delete = ON")
	if err != nil {
		return err
	}
	mgr.log("Activated database secure delete behaviour.")

	return nil
}

// initDB create a database to store the virtualizers or connect to the current one.
func (mgr *Manager) initDB() error {
	var err error

	err = mgr.createDB()
	if err != nil {
		return err
	}
	s := "CREATE TABLE IF NOT EXISTS {{.Table}} ({{.Name}} TEXT, {{.Type}} TEXT, {{.Data}} BLOB, PRIMARY KEY ({{.Name}}))"
	tmpl := template.Must(template.New("virtualizerTableInit").Parse(s))
	buf := new(bytes.Buffer)
	err = tmpl.Execute(buf, virtualizerTable)
	if err != nil {
		return err
	}

	query := buf.String()
	_, err = mgr.database.Exec(query)
	if err != nil {
		return err
	}
	mgr.log("Created virtualizer table.")

	return nil
}

// New creates a new manager the daemon can communicate with.
func New(args *ManagerArgs) (*Manager, error) {
	var err error
	var mgr *Manager

	mgr = new(Manager)
	mgr.log = args.Logger
	if mgr.log == nil {
		mgr.log = func(format string, v ...interface{}) {}
	}
	mgr.passphrase = args.Passphrase
	mgr.databaseAddr = args.DatabaseAddress
	mgr.firecrackerPath = args.FirecrackerPath
	// mgr.subserver = args.Subserver

	// Set drive to store vms if not provided default is temp
	mgr.vmdrive = args.VMDrive
	if mgr.vmdrive == "" {
		mgr.vmdrive = os.TempDir()
	} else {
		// make sure provided path has all directories
		err = os.MkdirAll(mgr.vmdrive, 0700)
		if err != nil {
			return nil, err
		}

	}
	if runtime.GOOS == "linux" {
		// make path if doesn't exist
		err = os.MkdirAll(mgr.firecrackerPath, 0700)
		if err != nil {
			return nil, err
		}
	}
	err = mgr.initDB()
	if err != nil {
		return nil, err
	}

	return mgr, nil
}

func (mgr *Manager) checkForCloseVirtualizer() error {
	var err error
	ActiveVMs.Range(func(key, value interface{}) bool {
		v, ok := value.(Virtualizer)
		if !ok {
			err = fmt.Errorf("unable to assert to virtualizer")
			return true
		}
		// pass boolean to force delete make the ctrl-c process faster
		err = v.Close(true)
		if err != nil {
			return true
		}
		return true
	})
	return err
}

// Close loops through the current active vms to close them as the manager is closing.
func (mgr *Manager) Close() error {
	var err error

	err = mgr.checkForCloseVirtualizer()
	if err != nil {
		return err
	}
	err = mgr.database.Close()
	if err != nil {
		return err
	}

	return err
}

// ListTuple is an object stored in the database easier to reference through a struct
type ListTuple struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// List returns an array of ListTuples from the Database
func (mgr *Manager) List() ([]ListTuple, error) {

	s := "SELECT {{.Name}}, {{.Type}} FROM {{.Table}} ORDER BY {{.Name}}"
	tmpl := template.Must(template.New("virtualizerTableInit").Parse(s))
	buf := new(bytes.Buffer)
	err := tmpl.Execute(buf, virtualizerTable)
	if err != nil {
		panic(err)
	}
	query := buf.String()
	rows, err := mgr.database.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list = make([]ListTuple, 0)
	for rows.Next() {
		var tuple ListTuple
		err = rows.Scan(&tuple.Name, &tuple.Type)
		if err != nil {
			return nil, err
		}
		list = append(list, tuple)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return list, nil
}

// DiskFormat returns the imageformat required for this virtualizer.
func (mgr *Manager) DiskFormat(name string) (vdisk.Format, error) {

	s := "SELECT {{.Type}} FROM {{.Table}} WHERE {{.Name}}=?"
	tmpl := template.Must(template.New("virtualizerTableInit").Parse(s))
	buf := new(bytes.Buffer)
	err := tmpl.Execute(buf, virtualizerTable)
	if err != nil {
		panic(err)
	}
	query := buf.String()
	row := mgr.database.QueryRow(query, name)
	var ptype string
	err = row.Scan(&ptype)
	if err == sql.ErrNoRows {
		return vdisk.RAWFormat, fmt.Errorf("no virtualizer named '%s'", name)
	}
	if err != nil {
		return vdisk.RAWFormat, err
	}

	palloc, ok := registeredVirtualizers[ptype]
	if !ok {
		return vdisk.RAWFormat, fmt.Errorf("virtualizer '%s' has unrecognized virtualizer type: %s", name, ptype)
	}

	return palloc.DiskFormat(), nil
}

// DiskAlignment returns the alignment the disk needs to be for the virtualizer
func (mgr *Manager) DiskAlignment(name string) (vcfg.Bytes, error) {
	s := "SELECT {{.Type}}, {{.Data}} FROM {{.Table}} WHERE {{.Name}}=?"
	tmpl := template.Must(template.New("virtualizerTableInit").Parse(s))
	buf := new(bytes.Buffer)
	err := tmpl.Execute(buf, virtualizerTable)
	if err != nil {
		panic(err)
	}
	query := buf.String()
	row := mgr.database.QueryRow(query, name)
	var ptype string
	var data []byte
	err = row.Scan(&ptype, &data)
	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("no virtualizer named '%s'", name)
	}
	if err != nil {
		return 0, err
	}

	palloc, ok := registeredVirtualizers[ptype]
	if !ok {
		return 0, fmt.Errorf("virtualizer '%s' has unrecognized virtualizer type: %s", name, ptype)
	}

	return palloc.DiskAlignment(), nil
}

// ReturnData returns data related to the virtualizer to show a user.
func (mgr *Manager) ReturnData(name string) ([]byte, error) {
	s := "SELECT {{.Type}}, {{.Data}} FROM {{.Table}} WHERE {{.Name}}=?"
	tmpl := template.Must(template.New("virtualizerTableInit").Parse(s))
	buf := new(bytes.Buffer)
	err := tmpl.Execute(buf, virtualizerTable)
	if err != nil {
		panic(err)
	}
	query := buf.String()
	row := mgr.database.QueryRow(query, name)
	var ptype string
	var data []byte

	err = row.Scan(&ptype, &data)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no virtualizer named '%s'", name)
	}
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ValidateArgs validates the arguments provided to see if the virtualizer is
func (mgr *Manager) ValidateArgs(name string) error {
	s := "SELECT {{.Type}}, {{.Data}} FROM {{.Table}} WHERE {{.Name}}=?"
	tmpl := template.Must(template.New("virtualizerTableInit").Parse(s))
	buf := new(bytes.Buffer)
	err := tmpl.Execute(buf, virtualizerTable)
	if err != nil {
		panic(err)
	}
	query := buf.String()
	row := mgr.database.QueryRow(query, name)
	var ptype string
	var data []byte
	err = row.Scan(&ptype, &data)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no virtualizer named '%s'", name)
	}
	if err != nil {
		return err
	}

	palloc, ok := registeredVirtualizers[ptype]
	if !ok {
		return fmt.Errorf("virtualizer '%s' has unrecognized virtualizer type: %s", name, ptype)
	}

	return palloc.ValidateArgs(data)
}

func (mgr *Manager) deleteVirtualizerData(tx *sql.Tx, name string) error {
	s := "DELETE FROM {{.Table}} WHERE {{.Name}}=?"
	tmpl := template.Must(template.New("virtualizerTableInit").Parse(s))
	buf := new(bytes.Buffer)
	err := tmpl.Execute(buf, virtualizerTable)
	if err != nil {
		return err
	}
	query := buf.String()
	_, err = tx.Exec(query, name)
	if err != nil {
		return err
	}
	err = tx.Commit()
	if err != nil {
		return err
	}
	return nil
}

//DeleteVirtualizer removes a virtualizer from the database with the appropriate name
func (mgr *Manager) DeleteVirtualizer(name string) error {
	tx, err := mgr.database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	err = mgr.selectVirtualizerData(tx, name)
	if err != nil {
		return err
	}

	err = mgr.deleteVirtualizerData(tx, name)
	if err != nil {
		return err
	}

	return nil
}

//RegisteredVirtualizers returns the map purely for testing the register function
func RegisteredVirtualizers() map[string]VirtualizerAllocator {
	return registeredVirtualizers
}

func (mgr *Manager) fetchVirtualizerData(ptype string, data []byte) (*sql.Tx, error) {
	palloc, ok := registeredVirtualizers[ptype]
	if !ok {
		return nil, fmt.Errorf("unrecognized virtualizer type: %s", ptype)
	}
	err := palloc.ValidateArgs(data)
	if err != nil {
		return nil, err
	}
	// add to db
	tx, err := mgr.database.Begin()
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func (mgr *Manager) selectVirtualizerData(tx *sql.Tx, name string) error {
	s := "SELECT {{.Type}} FROM {{.Table}} WHERE {{.Name}}=?"
	tmpl := template.Must(template.New("virtualizerTableInit").Parse(s))
	buf := new(bytes.Buffer)
	err := tmpl.Execute(buf, virtualizerTable)
	if err != nil {
		return err
	}
	query := buf.String()
	row := tx.QueryRow(query, name)

	var x string
	err = row.Scan(&x)
	if err == nil {
		return fmt.Errorf("virtualizer named '%s' already exists", name)
	}
	if err != sql.ErrNoRows {
		return err
	}
	return nil
}

func insertCreateVirtualizerData(tx *sql.Tx, name string, ptype string, data []byte) error {
	s := "INSERT INTO {{.Table}} ({{.Name}}, {{.Type}}, {{.Data}}) VALUES(?, ?, ?)"
	tmpl := template.Must(template.New("virtualizerTableInit").Parse(s))
	buf := new(bytes.Buffer)
	err := tmpl.Execute(buf, virtualizerTable)
	if err != nil {
		return err
	}
	query := buf.String()
	_, err = tx.Exec(query, name, ptype, data)
	if err != nil {
		return err
	}

	err = tx.Commit()
	if err != nil {
		return err
	}
	return nil
}

// CreateVirtualizer creates a virtualizer from the name, type and data required
func (mgr *Manager) CreateVirtualizer(name, ptype string, data []byte) error {
	tx, err := mgr.fetchVirtualizerData(ptype, data)
	if err != nil {
		return err
	}

	defer tx.Rollback()

	err = mgr.selectVirtualizerData(tx, name)
	if err != nil {
		return err
	}

	err = insertCreateVirtualizerData(tx, name, ptype, data)
	if err != nil {
		return err
	}
	return nil
}

func (mgr *Manager) prepareVirtualizerData(name string) ([]byte, string, error) {
	s := "SELECT {{.Type}}, {{.Data}} FROM {{.Table}} WHERE {{.Name}}=?"
	tmpl := template.Must(template.New("virtualizerTableInit").Parse(s))
	buf := new(bytes.Buffer)
	err := tmpl.Execute(buf, virtualizerTable)
	if err != nil {
		return nil, "", err
	}
	query := buf.String()
	row := mgr.database.QueryRow(query, name)
	var ptype string
	var data []byte
	err = row.Scan(&ptype, &data)
	if err == sql.ErrNoRows {
		return nil, "", fmt.Errorf("no virtualizer named '%s'", name)
	}
	if err != nil {
		return nil, "", err
	}
	return data, ptype, nil
}

// Prepare calls the prepare function of a virtualizer which sets up the ability to spawn a VM.
func (mgr *Manager) Prepare(name string, args *PrepareArgs) (*VirtualizeOperation, error) {

	data, ptype, err := mgr.prepareVirtualizerData(name)
	if err != nil {
		return nil, err
	}

	palloc, ok := registeredVirtualizers[ptype]
	if !ok {
		return nil, fmt.Errorf("virtualizer '%s' has unrecognized virtualizer type: %s", name, ptype)
	}
	p := palloc.Alloc()
	err = p.Initialize(data)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize virtualizer '%s': %v", name, err)
	}
	args.PName = name
	args.FCPath = mgr.firecrackerPath
	// args.Subserver = mgr.subserver
	args.VMDrive = mgr.vmdrive

	op := p.Prepare(args)
	return op, nil
}
