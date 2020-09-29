package virtualbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thanhpk/randstr"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
	"github.com/vorteil/vorteil/pkg/virtualizers/util"
)

// Virtualizer is a struct which will implement the interface so the manager can create VMs
type Virtualizer struct {
	id            string         // rando has for named pipes and folder names
	name          string         // name of vm
	pname         string         // name of virtualizer
	source        interface{}    // details about how the vm was made
	state         string         // status of vm
	headless      bool           // to display gui or not
	created       time.Time      // time the vm was created
	networkType   string         // type of network to spawn on
	networkDevice string         // type of network device to use
	folder        string         // folder to store vm details
	disk          *os.File       // disk of the machine
	serialLogger  *logger.Logger // serial logger for serial output of app
	logger        elog.Logger    // logger for the CLI
	// subServer *graph.Graph
	routes []virtualizers.NetworkInterface // api network interface that displays ports
	config *vcfg.VCFG                      // config for the vm
	sock   net.Conn                        // Connection to listen to for serial output

	vmdrive string // store disks in this directory

}

// Type returns the type of virtualizer
func (v *Virtualizer) Type() string {
	return VirtualizerID
}

// State returns the state of the virtualizer
func (v *Virtualizer) State() string {
	return v.state
}

// getState fetches the state to maintain a polling request incase it gets cleaned up from a different gui
func (v *Virtualizer) getState() (string, error) {
	cmd := exec.Command("VBoxManage", "showvminfo", v.name)

	stdout := new(bytes.Buffer)
	cmd.Stdout = stdout

	err := v.execute(cmd)
	if err != nil {
		if strings.Contains(err.Error(), "Could not find a registered machine named") || strings.Contains(err.Error(), "The object is not ready") {
			return "", nil
		}
		return "", err
	}

	stdoutStr := stdout.String()
	if len(stdoutStr) == 0 {
		return "", fmt.Errorf("failed to get stdout from state check")
	}

	var parsedState string
	lines := strings.Split(stdoutStr, "\n")
	for _, l := range lines {
		if strings.HasPrefix(l, "State:") {
			parsedState = strings.TrimSpace(strings.Split(strings.TrimPrefix(l, "State:"), "(")[0])
			break
		}
	}
	return parsedState, nil
}

// Stop stops the virtual machine
func (v *Virtualizer) Stop() error {
	v.logger.Debugf("Stopping VM")
	if v.state != virtualizers.Ready {
		v.state = virtualizers.Changing
		err := v.execute(exec.Command("VBoxManage", "controlvm", v.name, "acpipowerbutton"))
		if err != nil {
			if !strings.Contains(err.Error(), "100%") {
				return err
			}
		}
		count := 0
		// wait till in powered off state
		for {
			state, err := v.getState()
			if err != nil {
				return err
			}
			if state == "powered off" {
				break
			}
			if count > 10 {
				v.state = virtualizers.Broken
				v.logger.Errorf("Unable to stop virtual machine within 10 seconds powering off...")
				err = v.ForceStop()
				if err != nil {
					return err
				}
				break
			}
			count++
			time.Sleep(time.Second * 1)
		}
		v.state = virtualizers.Ready

	}
	return nil
}

// Start starts the virtual machine
func (v *Virtualizer) Start() error {
	v.logger.Debugf("Starting VM")
	switch v.State() {
	case "ready":
		v.state = virtualizers.Changing
		// This needs to be routined as its waiting for the pipe to start
		go v.initLogging()
		// go func() {
		args := "gui"
		if v.headless {
			args = "headless"
		}
		var startVM func() error
		startVM = func() error {
			cmd := exec.Command("VBoxManage", "startvm", v.name, "--type", args)
			err := v.execute(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "is already locked by a session (or being locked or unlocked)") {
					return startVM()
				}
				v.state = virtualizers.Broken
				return err
			}
			return nil

		}
		err := startVM()
		if err != nil {
			return err
		}
		if v.networkType != "nat" {
			go func() {
				v.routes = util.LookForIP(v.serialLogger, v.routes)
			}()
		}
		v.state = virtualizers.Alive
		// }()
	default:
		return fmt.Errorf("vm not in a state to be started currently in: %s", v.State())
	}
	return nil
}

// Download returns disk as file.File
func (v *Virtualizer) Download() (vio.File, error) {
	if !(v.state == virtualizers.Ready) {
		return nil, fmt.Errorf("virtual machine must be in state ready to be downloaded")
	}
	f, err := vio.LazyOpen(v.disk.Name())
	if err != nil {
		return nil, err
	}
	return f, nil
}

// ForceStop is only used when ctrl-cing the daemon as its the quickers way to unlock the machine to delete.
func (v *Virtualizer) ForceStop() error {
	err := v.execute(exec.Command("VBoxManage", "controlvm", v.name, "poweroff"))
	if err != nil {
		if !strings.Contains(err.Error(), "100%") {
			return err
		}
	}
	v.state = virtualizers.Ready
	return nil
}

// Details returns data to for the ConverToVM function on util
func (v *Virtualizer) Details() (string, string, string, []virtualizers.NetworkInterface, time.Time, *vcfg.VCFG, interface{}) {
	return v.name, v.pname, v.state, v.routes, v.created, v.config, v.source
}

// Close shuts down the virtual machine and cleans up the disk and folders
func (v *Virtualizer) Close(force bool) error {
	v.logger.Debugf("Deleting VM")
	if force && !(v.state == virtualizers.Ready) {
		err := v.ForceStop()
		if err != nil {
			return err
		}
	} else if !(v.state == virtualizers.Ready) {
		err := v.Stop()
		if err != nil {
			if !strings.Contains(err.Error(), "not currently running") {
				return err
			}
		}
	}
	v.state = virtualizers.Deleted

	var stopVM func() error
	stopVM = func() error {
		err := v.execute(exec.Command("VBoxManage", "unregistervm", v.name))
		if err != nil {
			if !strings.Contains(err.Error(),
				fmt.Sprintf("Could not find a registered machine named")) {
				// VM still shutting down
				if strings.Contains(err.Error(), "is already locked by a session (or being locked or unlocked)") {
					time.Sleep(time.Millisecond * 500)
					return stopVM()
				}
				return err
			}
		}
		return nil
	}
	err := stopVM()
	if err != nil {
		if !strings.Contains(err.Error(),
			fmt.Sprintf("Cannot unregister the machine '%s' while it is locked",
				v.name)) && !strings.Contains(err.Error(),
			fmt.Sprintf("Could not find a registered machine")) && !strings.Contains(err.Error(), "(MISSING)") {
			return err
		}
	}

	if v.sock != nil {
		v.sock.Close()
	}
	v.disk.Close()
	virtualizers.ActiveVMs.Delete(v.name)

	return nil
}

// Initialize passes the arguments from creation to create a virtualizer
func (v *Virtualizer) Initialize(data []byte) error {
	c := new(Config)
	err := c.Unmarshal(data)
	if err != nil {
		return err
	}

	v.headless = c.Headless
	v.networkType = c.NetworkType
	v.networkDevice = c.NetworkDevice
	return nil
}

// operation is the job progress that gets tracked via APIs
type operation struct {
	finishedLock sync.Mutex
	isFinished   bool
	*Virtualizer
	Logs   chan string
	Status chan string
	Error  chan error
	ctx    context.Context
}

// log writes a log to the channel for the job
func (o *operation) log(text string, v ...interface{}) {
	o.Logs <- fmt.Sprintf(text, v...)
}

// finished completes the operation and lets the user know and cleans up channels
func (o *operation) finished(err error) {
	o.finishedLock.Lock()
	defer o.finishedLock.Unlock()
	if o.isFinished {
		return
	}

	o.isFinished = true

	if err != nil {
		o.Logs <- fmt.Sprintf("Error: %v", err)
		o.Status <- fmt.Sprintf("Failed: %v", err)
		o.Error <- err
	}

	close(o.Logs)
	close(o.Status)
	close(o.Error)
}

// updateStatus updates the status of the job to provide more feedback to the user currently reading the job.
func (o *operation) updateStatus(text string) {
	o.Status <- text
	o.Logs <- text
}

// Serial returns the serial logger which contains the serial output of the app.
func (v *Virtualizer) Serial() *logger.Logger {
	return v.serialLogger
}

// Prepare prepares the virtualizer with the appropriate fields to run the virtual machine
func (v *Virtualizer) Prepare(args *virtualizers.PrepareArgs) *virtualizers.VirtualizeOperation {

	op := new(operation)
	op.Virtualizer = v
	v.name = args.Name
	v.pname = args.PName
	v.vmdrive = args.VMDrive
	// v.source = args.Source
	v.config = args.Config
	v.state = virtualizers.Changing
	v.created = time.Now()
	v.logger = args.Logger
	v.serialLogger = logger.NewLogger(2048 * 10)
	v.logger.Debugf("Preparing VM")
	v.routes = util.Routes(args.Config.Networks)

	op.Logs = make(chan string, 128)
	op.Error = make(chan error, 1)
	op.Status = make(chan string, 10)
	op.ctx = args.Context

	o := new(virtualizers.VirtualizeOperation)
	o.Logs = op.Logs
	o.Error = op.Error
	o.Status = op.Status

	go op.prepare(args)

	return o
}

// Detach ... removes vm from active vms list and moves content to source directory
// func (v *Virtualizer) Detach(source string) error {
// 	if v.state != virtualizers.Ready {
// 		return errors.New("virtual machine must be in a ready state to detach")
// 	}

// 	// remove "-" from the source directory as clonevm doesn't seem to work with it replace with space
// 	source = strings.ReplaceAll(source, "-", " ")
// 	// make directory to put it in as it doesn't exist anymore
// 	err := os.MkdirAll(source, 0777)
// 	if err != nil {
// 		return err
// 	}
// 	_, err = os.Stat(filepath.Join(filepath.ToSlash(source), fmt.Sprintf("%s Clone", v.name)))
// 	if err == nil {
// 		return errors.New("source directory where you want to copy already exists")
// 	} else if !os.IsNotExist(err) {
// 		return err
// 	}
// 	// clone vm from temp directory to source
// 	cmd := exec.Command("VBoxManage", "clonevm", v.name, fmt.Sprintf("--basefolder=%s", filepath.ToSlash(source)), "--register")
// 	err = v.execute(cmd)
// 	if err != nil {
// 		if !strings.Contains(err.Error(), "100%") {
// 			return err
// 		}
// 	}

// 	time.Sleep(time.Second * 5)
// 	// remove vm entirely from here might as well force it as the cloned vm has a non corrupt disk
// 	err = v.Close(true)
// 	if err != nil {
// 		return err
// 	}

// 	if runtime.GOOS != "windows" {
// 		cmd := exec.Command("VBoxManage", "modifyvm", fmt.Sprintf("%s Clone", v.name), "--uartmode1", "server", filepath.Join(filepath.ToSlash(source), fmt.Sprintf("%s Clone", v.name), "monitor.sock"))
// 		err = v.execute(cmd)
// 		if err != nil {
// 			if !strings.Contains(err.Error(), "100%") {
// 				return err
// 			}
// 		}
// 	}

// 	return nil
// }

// execute is generic wrapping function to run command execs
func (v *Virtualizer) execute(cmd *exec.Cmd) error {
	if !strings.Contains(strings.Join(cmd.Args, " "), "showvminfo") {
		v.logger.Infof("Executing %s", strings.Join(cmd.Args, " "))
	}

	stderr := new(bytes.Buffer)
	cmd.Stderr = stderr

	err := cmd.Start()
	if err != nil {
		return err
	}

	e := cmd.Wait()
	if len(stderr.String()) == 0 {
		return e
	}

	return fmt.Errorf(stderr.String())
}

// checkState polls getState for a state monitoring solution when an app crashes
func (v *Virtualizer) checkState() {
	for {
		state, err := v.getState()
		if err != nil {
			if !strings.Contains(err.Error(), "signal: interrupt") || !strings.Contains(err.Error(), "Could not find a registered machine") || !strings.Contains(err.Error(), "exit status 3221225786") || !strings.Contains(err.Error(), "The object is not ready") {
				v.logger.Errorf("Getting VM State: %s", err.Error())
			}
		}
		if state == "" {
			break
		}
		if state == "running" {
			v.state = virtualizers.Alive
		}
		if state == "powered off" {
			v.state = virtualizers.Ready
		}
		time.Sleep(time.Second * 1)
	}
}

func createVM(basefolder, name string) []string {
	return []string{"createvm", "--basefolder", basefolder, "--name", name, "--register"}
}

func modifyVM(name string, ram, cpus, sock string) []string {
	vboxArgs := []string{"modifyvm", name,
		"--memory", ram, "--acpi", "on",
		"--ioapic", "on", "--cpus", cpus,
		"--longmode", "on", "--largepages", "on", "--chipset", "ich9",
		"--bioslogofadein", "off", "--bioslogofadeout", "off",
		"--bioslogodisplaytime", "1", "--biosbootmenu", "disabled",
		"--rtcuseutc", "on", "--uart1", "0x3F8", "4", "--uartmode1",
		"server", sock}

	if runtime.GOOS == "windows" {
		hyperVEnabled := false
		virts, _ := virtualizers.Backends()
		for _, v := range virts {
			if v == "hyperv" {
				hyperVEnabled = true
			}
		}
		if !hyperVEnabled {
			vboxArgs = append(vboxArgs, "--nested-hw-virt", "on")
		}
	} else {
		vboxArgs = append(vboxArgs, "--nested-hw-virt", "on")
	}
	return vboxArgs
}

// prepareVM executes the modify function with appropriate arguments for storage
func (v *Virtualizer) prepareVM(diskpath string) error {
	cpus := int(v.config.VM.CPUs)
	if cpus == 0 {
		cpus = 1
	}
	mVMArgs := modifyVM(v.name, strconv.Itoa(v.config.VM.RAM.Units(vcfg.MiB)), strconv.Itoa(cpus), filepath.ToSlash(filepath.Join(v.folder, "monitor.sock")))
	if runtime.GOOS == "windows" {
		mVMArgs = modifyVM(v.name, strconv.Itoa(v.config.VM.RAM.Units(vcfg.MiB)), strconv.Itoa(cpus), fmt.Sprintf("\\\\.\\pipe\\%s", v.id))
	}
	cmd := exec.Command("VBoxManage", mVMArgs...)
	err := v.execute(cmd)
	if err != nil {
		return err
	}

	cmd = exec.Command("VBoxManage", "storagectl", v.name,
		"--name", fmt.Sprintf("SCSI-%s", filepath.Base(diskpath)), "--add", "virtio-scsi", "--portcount", "16",
		"--bootable", "on")
	err = v.execute(cmd)
	if err != nil {
		return err
	}

	cmd = exec.Command("VBoxManage", "storageattach", v.name,
		"--storagectl", fmt.Sprintf("SCSI-%s", filepath.Base(diskpath)), "--port", "0", "--device", "0",
		"--type", "hdd", "--medium", diskpath)
	err = v.execute(cmd)
	if err != nil {
		return err
	}

	return nil
}

func (v *Virtualizer) Bind(args []string, i int, j int, protocol string, port virtualizers.RouteMap, networkType string) ([]string, string, bool, error) {
	var hasDefinedPorts bool
	bind, nr, err := virtualizers.BindPort(v.networkType, protocol, port.Port)
	if err != nil {
		return nil, "", false, err
	}
	hasDefinedPorts = true
	args = append(args, fmt.Sprintf("--natpf%s", strconv.Itoa(i+1)))
	args = append(args, fmt.Sprintf("nat%s%s,%s,,%s,,%s", bind, networkType, protocol, bind, port.Port))
	return args, nr, hasDefinedPorts, nil
}

func (v *Virtualizer) handlePorts(args []string, route virtualizers.NetworkInterface, protocol string, i int) ([]string, bool, error) {
	var err error
	var nr string
	var hasDefinedPorts bool
	for j, port := range route.HTTP {
		args, nr, hasDefinedPorts, err = v.Bind(args, i, j, protocol, port, "http")
		if err != nil {
			return nil, false, err
		}
		v.routes[i].HTTP[j].Address = nr
	}
	for j, port := range route.HTTPS {
		args, nr, hasDefinedPorts, err = v.Bind(args, i, j, protocol, port, "https")
		if err != nil {
			return nil, false, err
		}
		v.routes[i].HTTPS[j].Address = nr
	}
	for j, port := range route.TCP {
		args, nr, hasDefinedPorts, err = v.Bind(args, i, j, protocol, port, "tcp")
		if err != nil {
			return nil, false, err
		}
		v.routes[i].TCP[j].Address = nr
	}
	for j, port := range route.UDP {
		args, nr, hasDefinedPorts, err = v.Bind(args, i, j, protocol, port, "udp")
		if err != nil {
			return nil, false, err
		}
		v.routes[i].UDP[j].Address = nr
	}
	return args, hasDefinedPorts, nil
}

func (v *Virtualizer) gatherNetworkDetails() error {
	hasDefinedPorts := false
	var noNic int
	var err error
	for i, route := range v.routes {
		args := make([]string, 0)
		args = append(args, "modifyvm", v.name)
		noNic++
		protocol := "tcp"
		if v.networkType == "nat" {
			var nargs []string

			nargs, hasDefinedPorts, err = v.handlePorts(args, route, protocol, i)
			if err != nil {
				return err
			}
			if hasDefinedPorts {
				cmd := exec.Command("VBoxManage", nargs...)
				err := v.execute(cmd)
				if err != nil {
					return err
				}
			}
		}
		if len(v.routes) != 0 && !hasDefinedPorts {
			v.logger.Warnf("Warning VM has network cards but no defined ports")
		}
	}

	args := make([]string, 0)
	args = append(args, "modifyvm", v.name)

	for i := 1; i <= noNic; i++ {
		args = append(args, "--nic"+strconv.Itoa(i), v.networkType)

		switch v.networkType {
		case "bridged":
			args = append(args, "--bridgeadapter"+strconv.Itoa(i), v.networkDevice)
		case "hostonly":
			args = append(args, "--hostonlyadapter"+strconv.Itoa(i), v.networkDevice)
		default:
		}
		args = append(args, "--nictype"+strconv.Itoa(i), "virtio", "--cableconnected"+strconv.Itoa(i), "on")
	}

	cmd := exec.Command("VBoxManage", args...)
	err = v.execute(cmd)
	if err != nil {
		return err
	}
	return nil
}

func (v *Virtualizer) createAndConfigure(diskpath string) error {
	cVMArgs := createVM(v.folder, v.name)
	cmd := exec.Command("VBoxManage", cVMArgs...)
	err := v.execute(cmd)
	if err != nil {
		return err
	}

	err = v.prepareVM(diskpath)
	if err != nil {
		return err
	}

	cmd = exec.Command("VBoxManage", "setextradata", v.name,
		"VBoxInternal/Devices/serial/0/Config/YieldOnLSRRead", "1")
	err = v.execute(cmd)
	if err != nil {
		return err
	}

	err = v.gatherNetworkDetails()
	if err != nil {
		return err
	}
	return nil
}

// checkIfBridged checks if bridged device exists on virtualbox
func (o *operation) checkIfBridged() (bool, error) {
	var deviceIsBridged bool
	if o.networkType == "bridged" {
		devices, err := virtualizers.BridgedDevices()
		if err != nil {
			return false, err
		}
		for _, device := range devices {
			if device == o.networkDevice {
				deviceIsBridged = true
				break
			}
		}
		if !deviceIsBridged {
			return false, fmt.Errorf("error: network device '%s' is not a valid bridge interface", o.networkDevice)
		}
	}
	return deviceIsBridged, nil
}

// checkIfHost checks if host device exists on virtualbox
func (o *operation) checkIfHost() (bool, error) {
	var deviceIsHost bool
	if o.networkType == "hostonly" {
		devices, err := virtualizers.HostDevices()
		if err != nil {
			return false, err
		}
		for _, device := range devices {
			if device == o.networkDevice {
				deviceIsHost = true
				break
			}
		}
		if !deviceIsHost {
			return false, fmt.Errorf("error: network device '%s' is not a valid host interface", o.networkDevice)

		}
	}
	return deviceIsHost, nil
}

// prepare sets the fields and arguments to spawn the virtual machine
func (o *operation) prepare(args *virtualizers.PrepareArgs) {
	var returnErr error
	var err error

	o.updateStatus(fmt.Sprintf("Preparing virtualbox files..."))
	defer func() {
		o.finished(returnErr)
	}()

	o.name = args.Name
	o.id = randstr.Hex(5)
	o.folder = filepath.Dir(args.ImagePath)

	_, err = o.checkIfBridged()
	if err != nil {
		returnErr = err
		return
	}

	_, err = o.checkIfHost()
	if err != nil {
		returnErr = err
		return
	}

	_, loaded := virtualizers.ActiveVMs.LoadOrStore(o.name, o.Virtualizer)
	if loaded {
		returnErr = errors.New("virtual machine already exists")
		return
	}

	err = o.startupVM(args.ImagePath, args.Start)
	if err != nil {
		returnErr = err
		return
	}

}

func (o *operation) startupVM(path string, start bool) error {
	err := o.createAndConfigure(path)
	if err != nil {
		return fmt.Errorf("Error configuring vm: %s", err.Error())
	}

	o.state = "ready"
	go o.checkState()

	if start {
		err = o.Start()
		if err != nil {
			return fmt.Errorf("Error starting vm: %s", err.Error())
		}
	}
	return nil
}
