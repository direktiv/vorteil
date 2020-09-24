// +build linux

package firecracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	dhcp "github.com/krolaw/dhcp4"
	conn "github.com/krolaw/dhcp4/conn"
	"github.com/milosgajdos/tenus"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"github.com/songgao/water"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	dhcpHandler "github.com/vorteil/vorteil/pkg/virtualizers/dhcp"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
	"github.com/vorteil/vorteil/pkg/virtualizers/util"
)


const (
	vorteilBridge = "vorteil-bridge"
)


// FetchBridgeDev attempts to retrieve the bridge device
func FetchBridgeDev() error {
	// Check if bridge device exists
	_, err := tenus.BridgeFromName(vorteilBridge)
	if err != nil {
		return errors.New("try running 'vorteil firecracker-setup' before using firecracker")
	}
	return err
}


// SetupBridgeAndDHCPServer initializes a dhcp server, bridge device and a http listener to create TAP devices
func SetupBridgeAndDHCPServer() error {

// SetupBridgeAndDHCPServer creates the bridge which provides DHCP addresses todo
// firecracker instances.
func SetupBridgeAndDHCPServer(log elog.View) error {

	log.Printf("creating bridge %s", vorteilBridge)
	// Create bridge device
	bridger, err := tenus.NewBridgeWithName(vorteilBridge)
	if err != nil {
		if !strings.Contains(err.Error(), "Interface name vorteil-bridge already assigned on the host") {
			return err
		}
		// get bridge device
		bridger, err = tenus.BridgeFromName(vorteilBridge)
		if err != nil {
			return err
		}
	}
	// Switch bridge up
	if err = bridger.SetLinkUp(); err != nil {
		return err
	}
	// Fetch address
	ipv4Addr, ipv4Net, err := net.ParseCIDR("174.72.0.1/24")
	if err != nil {
		if !strings.Contains(err.Error(), "file exists") {
			return err
		}
	}
	// Assign bridge to device so host knows where to send requests.
	if err = bridger.SetLinkIp(ipv4Addr, ipv4Net); err != nil {
		if !strings.Contains(err.Error(), "file exists") {
			return err
		}
	}

	log.Printf("starting dhcp server")

	// create dhcp server on an interface
	server := dhcpHandler.NewHandler()
	pc, err := conn.NewUDP4BoundListener(vorteilBridge, ":67")
	if err != nil {
		return err
	}

	// create server handler to create tap devices under sudo
	http.HandleFunc("/", OrganiseTapDevices)
	go func() {
		http.ListenAndServe(":7476", nil)
	}()
	fmt.Printf("Listening on '7476' for creating and deleting TAP devices\n")
	fmt.Printf("Listening on 'vorteil-bridge' for DHCP requests")
	// Start dhcp server to listen
	dhcp.Serve(pc, server)

	return nil
}

// CreateDevices is a struct used to tell the process to create TAP devices via a rest request
type CreateDevices struct {
	Id     string `json:"id"`
	Routes int    `json:"count"`
}

// Devices is a struct used to tell the process to deleted TAP devices via a delete request
type Devices struct {
	Devices []string `json:"devices"`
}

// OrganiseTapDevices handles http requests to create and delete tap interfaces for firecracker
func OrganiseTapDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var cd CreateDevices
		var tapDevices []string

		err := json.NewDecoder(r.Body).Decode(&cd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		// get bridge device
		bridgeDev, err := tenus.BridgeFromName(vorteilBridge)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		// set network adapters
		if cd.Routes > 0 {
			for i := 0; i < cd.Routes; i++ {
				ifceName := fmt.Sprintf("%s-%s", cd.Id, strconv.Itoa(i))

				// create tap device
				config := water.Config{
					DeviceType: water.TAP,
				}
				config.Name = ifceName
				config.Persist = true
				ifce, err := water.New(config)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}
				// close interface so firecracker can read it
				err = ifce.Close()
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}

				// get tap device
				linkDev, err := tenus.NewLinkFrom(ifceName)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}
				//set tap device up
				err = linkDev.SetLinkUp()
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}
				// add network interface to bridge
				err = bridgeDev.AddSlaveIfc(linkDev.NetInterface())
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}
				tapDevices = append(tapDevices, ifceName)
			}
			// write interfaces back
			returnDevices := &Devices{
				Devices: tapDevices,
			}
			body, err := json.Marshal(returnDevices)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)

			}
			io.Copy(w, bytes.NewBuffer(body))
		}
	case http.MethodDelete:
		var dd Devices
		err := json.NewDecoder(r.Body).Decode(&dd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		for i := 0; i < len(dd.Devices); i++ {
			err := tenus.DeleteLink(dd.Devices[i])
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not available", http.StatusBadRequest)
	}
}

// DownloadPath is the path where we pull firecracker-vmlinux's from
const DownloadPath = "https://storage.googleapis.com/vorteil-dl/firecracker-vmlinux/"

// Details returns data to for the ConverToVM function on util
func (v *Virtualizer) Details() (string, string, string, []virtualizers.NetworkInterface, time.Time, *vcfg.VCFG, interface{}) {
	return v.name, v.pname, v.state, v.routes, v.created, v.config, v.source
}

// Virtualizer is a struct which will implement the interface so the manager can control it
type Virtualizer struct {
	// VM related stuff
	id    string // random hash for folder names
	name  string // name of vm
	pname string // name of virtualizer
	state string // status of vm

	created      time.Time      // time the vm was created
	folder       string         // folder to store vm details
	disk         *os.File       // disk of the machine
	source       interface{}    // details about how the vm was made
	kip          string         // vmlinux full path
	logger       elog.View      // logger
	serialLogger *logger.Logger // logs for the serial of the vm

	routes []virtualizers.NetworkInterface // api network interface that displays ports
	config *vcfg.VCFG                      // config for the vm

	firecrackerPath string // path to vmlinux files
	// firecracker related objects
	vmmCtx    context.Context    // virtualmachineManager firecracker context
	gctx      context.Context    // global context used to spawn vmm
	vmmCancel context.CancelFunc // cancel vmm

	fconfig     firecracker.Config   // config for virtual machine manager
	machine     *firecracker.Machine // machine firecracker spawned
	machineOpts []firecracker.Opt    // options provided to spawn machine

	bridgeDevice tenus.Bridger // bridge device e.g vorteil-bridge
	tapDevice    Devices       // tap device for the machine

	vmdrive string // store disks in this directory
}

// Detach ... Potentially Todo i think firecracker detach is alot more complicated because of the tap devices
// func (v *Virtualizer) Detach(source string) error {
// 	if v.state != virtualizers.Ready {
// 		return errors.New("virtual machine must be in ready state to detach")
// 	}
// 	name := filepath.Base(v.folder)

// 	err := os.MkdirAll(filepath.Join(source), 0777)
// 	if err != nil {
// 		return err
// 	}
// 	err = os.Rename(v.folder, filepath.Join(source, name))
// 	if err != nil {
// 		return err
// 	}

// 	v.config.VM.RAM.Align(vcfg.MiB * 2)

// 	// close and cleanup tap devices
// 	// stopVMM
// 	err = v.machine.StopVMM()
// 	if err != nil {
// 		return err
// 	}

// 	v.state = virtualizers.Deleted

// 	cleanup, err := os.Create(filepath.Join(source, name, "cleanup.sh"))
// 	if err != nil {
// 		return err
// 	}
// 	defer cleanup.Close()
// 	var tapArgs []string
// 	var tapCreation []string
// 	type NetworkInterface struct {
// 		IfaceID     string `json:"iface_id"`
// 		HostDevName string `json:"host_dev_name"`
// 	}
// 	var networkCards []NetworkInterface
// 	// write script for Tap setup
// 	if len(v.routes) > 0 {
// 		for i := 0; i < len(v.routes); i++ {
// 			ifceName := fmt.Sprintf("%s-%s", v.id, strconv.Itoa(i))
// 			tapCreation = append(tapCreation, ifceName)
// 		}
// 		for _, tap := range tapCreation {
// 			tapArgs = append(tapArgs, fmt.Sprintf("sudo ip tuntap add dev %s mode tap", tap))
// 			tapArgs = append(tapArgs, fmt.Sprintf("sudo brctl addif vorteil-bridge %s", tap))
// 			tapArgs = append(tapArgs, fmt.Sprintf("sudo ip link set dev %s up", tap))
// 			cleanup.Write([]byte(fmt.Sprintf("sudo ip link delete %s\n", tap)))

// 			networkCards = append(networkCards, NetworkInterface{
// 				IfaceID:     "vorteil-bridge",
// 				HostDevName: tap,
// 			})
// 		}
// 	}
// 	f, err := os.Create(filepath.Join(source, name, "interfaces.sh"))
// 	if err != nil {
// 		return err
// 	}
// 	f.Write([]byte(strings.Join(tapArgs, "\n")))
// 	defer f.Close()

// 	type Drive struct {
// 		DriveID      string `json:"drive_id"`
// 		PathOnHost   string `json:"path_on_host"`
// 		IsRootDevice bool   `json:"is_root_device"`
// 		IsReadOnly   bool   `json:"is_read_only"`
// 	}
// 	type BootSource struct {
// 		KernelImagePath string `json:"kernel_image_path"`
// 		BootArgs        string `json:"boot_args"`
// 	}
// 	type MachineConfig struct {
// 		VcpuCount  int  `json:"vcpu_count"`
// 		MemSizeMib int  `json:"mem_size_mib"`
// 		HtEnabled  bool `json:"ht_enabled"`
// 	}
// 	type fcConfig struct {
// 		BootSource        BootSource         `json:"boot-source"`
// 		Drives            []Drive            `json:"drives"`
// 		MachineConfig     MachineConfig      `json:"machine-config"`
// 		NetworkInterfaces []NetworkInterface `json:"network-interfaces"`
// 	}

// 	drive := Drive{
// 		DriveID:      "rootfs",
// 		PathOnHost:   filepath.Join(source, name, fmt.Sprintf("%s.raw", v.name)),
// 		IsRootDevice: true,
// 		IsReadOnly:   false,
// 	}
// 	var drives []Drive
// 	drives = append(drives, drive)

// 	var config fcConfig
// 	config.Drives = drives
// 	config.BootSource = BootSource{
// 		KernelImagePath: v.kip,
// 		BootArgs:        "init=/vorteil/vinitd reboot=k panic=1 pci=off i8042.noaux i8042.nomux i8042.nopnp i8042.dumbkbd  vt.color=0x00",
// 	}
// 	config.MachineConfig = MachineConfig{
// 		VcpuCount:  int(v.config.VM.CPUs),
// 		HtEnabled:  false,
// 		MemSizeMib: v.config.VM.RAM.Units(vcfg.MiB),
// 	}
// 	config.NetworkInterfaces = networkCards

// 	data, err := json.Marshal(config)
// 	if err != nil {
// 		return err
// 	}

// 	jf, err := os.Create(filepath.Join(source, name, "config.json"))
// 	if err != nil {
// 		return err
// 	}
// 	defer jf.Close()
// 	jf.Write(data)

// 	start, err := os.Create(filepath.Join(source, name, "start.sh"))
// 	if err != nil {
// 		return err
// 	}
// 	defer start.Close()
// 	start.Write([]byte("sudo ./interfaces.sh\nfirecracker --api-sock ./firecracker.socket --config-file ./config.json"))

// 	// Chmod scripts
// 	err = os.Chmod(start.Name(), 0777)
// 	if err != nil {
// 		return err
// 	}
// 	err = os.Chmod(f.Name(), 0777)
// 	if err != nil {
// 		return err
// 	}
// 	err = os.Chmod(cleanup.Name(), 0777)
// 	if err != nil {
// 		return err
// 	}

// 	// remove virtualizer from active vms
// 	virtualizers.ActiveVMs.Delete(v.name)
// 	return nil
// }

// Type returns the type of virtualizer
func (v *Virtualizer) Type() string {
	return VirtualizerID
}

// Initialize passes the arguments from creation to create a virtualizer. No arguments apart from name so no need to do anything
func (v *Virtualizer) Initialize(data []byte) error {
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

// writeCounter counts the number of bytes written to it.
type writeCounter struct {
	total      int64 // total size
	downloaded int64 // downloaded # of bytes transferred
	onProgress func(downloaded int64, total int64)
}

// Write implements the io.Writer interface.
//
// Always completes and never returns an error.
func (wc *writeCounter) Write(p []byte) (n int, e error) {
	n = len(p)
	wc.downloaded += int64(n)
	wc.onProgress(wc.downloaded, wc.total)
	return
}
func newWriter(size int64, onProgress func(downloaded, total int64)) io.Writer {
	return &writeCounter{total: size, onProgress: onProgress}
}

// fetchVMLinux reads the kernel it wants to run and returns the vm linux required to run
// Will download the vmlinux if it doesn't exist
func (o *operation) fetchVMLinux(kernel string) (string, error) {
	o.updateStatus(fmt.Sprintf("Fetching VMLinux searching %s for %s", o.firecrackerPath, kernel))
	// check if vmlinux is on system at valid path
	_, err := os.Stat(filepath.Join(o.firecrackerPath, kernel))
	if err != nil {
		// file doesn't exist must download from bucket
		o.updateStatus(fmt.Sprintf("VMLinux for kernel doesn't exist downloading..."))
		// Download vmlinux from google
		url := DownloadPath + kernel
		client := http.DefaultClient
		// Create file locally to download
		file, err := os.Create(filepath.Join(o.firecrackerPath, kernel))
		if err != nil {
			return "", err
		}
		defer file.Close()

		// Determinate the file size
		resp, err := client.Head(url)
		if err != nil {
			os.Remove(file.Name())
			return "", err
		}
		if resp.StatusCode == 404 {
			os.Remove(file.Name())
			return "", fmt.Errorf("Kernel '%s' VMLinux does not exist", kernel)
		}
		contentLength := resp.Header.Get("content-length")
		length, err := strconv.Atoi(contentLength)
		if err != nil {
			os.Remove(file.Name())
			return "", err
		}

		// Make request
		resp, err = client.Get(url)
		if err != nil {
			os.Remove(file.Name())
			return "", err
		}
		defer resp.Body.Close()
		p := o.logger.NewProgress("Downloading VMLinux", "Bytes", int64(length))
		defer p.Finish(false)
		// pipe stream
		var pDownloaded = int64(0)
		body := io.TeeReader(resp.Body, newWriter(int64(length), func(downloaded, total int64) {
			p.Increment(downloaded - pDownloaded)
			pDownloaded = downloaded
		}))
		_, err = io.Copy(file, body)
		if err != nil {
			os.Remove(file.Name())
			return "", err
		}

	}

	return filepath.Join(o.firecrackerPath, kernel), nil
}

// log writes a log to the channel for the job
func (o *operation) log(text string, v ...interface{}) {
	o.Logs <- fmt.Sprintf(text, v...)
}

// finished finishes the job and cleans up the channels
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

// updateStatus updates the status of the job to provide more feedback to the user
func (o *operation) updateStatus(text string) {
	o.Status <- text
	o.Logs <- text
}

// Serial returns the serial logger which contains the serial output of the application
func (v *Virtualizer) Serial() *logger.Logger {
	return v.serialLogger
}

// Stop stops the vm and changes it back to ready
func (v *Virtualizer) Stop() error {
	// Error might've happened before in the prepare so machine would be nil
	if v.machine != nil {
		v.logger.Debugf("Stopping VM")
		if v.state != virtualizers.Ready {
			v.state = virtualizers.Changing

			err := v.machine.Shutdown(v.vmmCtx)
			if err != nil {
				return err
			}

		} else {
			return errors.New("vm is already stopped")
		}
	}
	return nil
}

// State returns the state of the virtual machine
func (v *Virtualizer) State() string {
	return v.state
}

// Download returns the disk
func (v *Virtualizer) Download() (vio.File, error) {
	v.logger.Debugf("Downloading Disk")
	if !(v.state == virtualizers.Ready) {
		return nil, fmt.Errorf("the machine must be in a stopped or ready state")
	}

	f, err := vio.LazyOpen(v.disk.Name())
	if err != nil {
		return nil, err
	}

	return f, nil
}

// Close shuts down the virtual machine and cleans up the disk and folders
func (v *Virtualizer) Close(force bool) error {
	// Error might've happened before in the prepare so machine would be nil
	if v.machine != nil {
		v.logger.Debugf("Deleting VM")

		if !force {
			// if state not ready stop it so it is
			if !(v.state == virtualizers.Ready) {
				// stop
				err := v.Stop()
				if err != nil {
					return err
				}
			}
		}

		// stopVMM
		err := v.machine.StopVMM()
		if err != nil {
			return err
		}

		client := &http.Client{}
		cdm, err := json.Marshal(v.tapDevice)
		if err != nil {
			return err
		}

		req, err := http.NewRequest("DELETE", "http://localhost:7476/", bytes.NewBuffer(cdm))
		if err != nil {
			return err
		}

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		v.state = virtualizers.Deleted

		// remove virtualizer from active vms
		virtualizers.ActiveVMs.Delete(v.name)
	}

	return nil
}

// ConvertToVM is a wrapper function that provides us abilities to use the old APIs
func (v *Virtualizer) ConvertToVM() interface{} {
	info := v.config.Info
	vm := v.config.VM
	system := v.config.System
	programs := make([]virtualizers.ProgramSummaries, 0)

	for _, p := range v.config.Programs {
		programs = append(programs, virtualizers.ProgramSummaries{
			Binary: p.Binary,
			Args:   string(p.Args),
			Env:    p.Env,
		})
	}

	machine := &virtualizers.VirtualMachine{
		ID:       v.name,
		Author:   info.Author,
		CPUs:     int(vm.CPUs),
		RAM:      vm.RAM,
		Disk:     vm.DiskSize,
		Created:  v.created,
		Date:     info.Date.Time(),
		Networks: v.routes,
		Kernel:   vm.Kernel,
		Name:     info.Name,
		Summary:  info.Summary,
		Source:   v.source.(virtualizers.Source),
		URL:      string(info.URL),
		Version:  info.Version,
		Programs: programs,
		Hostname: system.Hostname,
		Platform: v.pname,
		Status:   v.state,
	}

	return machine
}

// Prepare prepares the virtualizer with the appropriate fields to run the virtualizer
func (v *Virtualizer) Prepare(args *virtualizers.PrepareArgs) *virtualizers.VirtualizeOperation {

	op := new(operation)
	op.Virtualizer = v
	v.name = args.Name
	v.pname = args.PName
	v.vmdrive = args.VMDrive
	v.firecrackerPath = args.FCPath

	v.created = time.Now()
	v.config = args.Config
	v.source = args.Source
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

// Write method to handle logging from firecracker to use our logger interface
// Cant use logger interface as it duplicates
func (v *Virtualizer) Write(d []byte) (n int, err error) {
	n = len(d)
	v.logger.Infof(string(d))
	return
}

type firecrackerFormatter struct {
	logrus.TextFormatter
}

func (f *firecrackerFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	return []byte(entry.Message), nil
}

// prepare sets the fields and arguments to spawn the virtual machine
func (o *operation) prepare(args *virtualizers.PrepareArgs) {
	var returnErr error

	o.updateStatus(fmt.Sprintf("Building firecracker command and tap interfaces..."))
	defer func() {
		o.finished(returnErr)
	}()

	o.state = "initializing"
	o.name = args.Name
	err := os.MkdirAll(args.FCPath, os.ModePerm)
	if err != nil {
		returnErr = err
		return
	}
	o.folder = filepath.Dir(args.ImagePath)
	o.id = strings.Split(filepath.Base(o.folder), "-")[1]
	diskpath := filepath.ToSlash(args.ImagePath)

	logger := log.New()
	logger.SetFormatter(&firecrackerFormatter{log.TextFormatter{
		DisableColors: false,
		ForceColors:   true,
	}})
	logger.Out = o

	ctx := context.Background()
	vmmCtx, vmmCancel := context.WithCancel(ctx)
	devices := []models.Drive{}

	rootDrive := models.Drive{
		DriveID:      firecracker.String("1"),
		PathOnHost:   &diskpath,
		IsRootDevice: firecracker.Bool(true),
		IsReadOnly:   firecracker.Bool(false),
		Partuuid:     vimg.Part2UUIDString,
	}

	devices = append(devices, rootDrive)

	o.kip, err = o.fetchVMLinux(fmt.Sprintf("firecracker-%s", o.config.VM.Kernel))
	if err != nil {
		returnErr = err
		return
	}

	cd := CreateDevices{
		Id:     o.id,
		Routes: len(o.routes),
	}

	cdm, err := json.Marshal(cd)
	if err != nil {
		returnErr = err
		return
	}
	resp, err := http.Post("http://localhost:7476/", "application/json", bytes.NewBuffer(cdm))
	if err != nil {
		returnErr = errors.New("Run 'sudo vorteil firecracker-setup' for the listener")
		return
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		returnErr = err
		return
	}
	var ifs Devices
	err = json.Unmarshal(body, &ifs)
	if err != nil {
		returnErr = err
		return
	}

	o.tapDevice = ifs
	var interfaces []firecracker.NetworkInterface

	for i := 0; i < len(ifs.Devices); i++ {
		interfaces = append(interfaces,
			firecracker.NetworkInterface{
				StaticConfiguration: &firecracker.StaticNetworkConfiguration{
					HostDevName: ifs.Devices[i],
				},
			},
		)
	}

	fcCfg := firecracker.Config{
		SocketPath:      filepath.Join(o.folder, fmt.Sprintf("%s.%s", o.name, "socket")),
		KernelImagePath: o.kip,
		KernelArgs:      fmt.Sprintf("loglevel=9 init=/vorteil/vinitd root=PARTUUID=%s reboot=k panic=1 pci=off vt.color=0x00", vimg.Part2UUIDString),
		Drives:          devices,
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(int64(o.config.VM.CPUs)),
			HtEnabled:  firecracker.Bool(false),
			MemSizeMib: firecracker.Int64(int64(o.config.VM.RAM.Units(vcfg.MiB))),
		},
		NetworkInterfaces: interfaces,
		ForwardSignals:    []os.Signal{},
	}

	machineOpts := []firecracker.Opt{
		firecracker.WithLogger(log.NewEntry(logger)),
	}

	// append new fields to overarching struct
	o.gctx = ctx
	o.vmmCtx = vmmCtx
	o.vmmCancel = vmmCancel
	o.machineOpts = machineOpts
	o.fconfig = fcCfg

	o.state = "ready"

	_, loaded := virtualizers.ActiveVMs.LoadOrStore(o.name, o.Virtualizer)
	if loaded {
		returnErr = errors.New("virtual machine already exists")
	}

	if args.Start {
		err = o.Start()
		if err != nil {
			returnErr = err
		}
	}
}

// Start create the virtualmachine and runs it
func (v *Virtualizer) Start() error {
	v.logger.Debugf("Starting VM")
	switch v.State() {
	case "ready":
		v.state = virtualizers.Changing

		go func() {
			executable, err := virtualizers.GetExecutable(VirtualizerID)
			if err != nil {
				v.logger.Errorf("Error Fetching executable: %s", err.Error())
			}

			cmd := firecracker.VMCommandBuilder{}.WithBin(executable).WithSocketPath(v.fconfig.SocketPath).WithStdout(v.serialLogger).WithStderr(v.serialLogger).Build(v.gctx)
			cmd.SysProcAttr = &syscall.SysProcAttr{
				Setpgid: true,
				Pgid:    0,
			}

			v.machineOpts = append(v.machineOpts, firecracker.WithProcessRunner(cmd))

			v.machine, err = firecracker.NewMachine(v.vmmCtx, v.fconfig, v.machineOpts...)
			if err != nil {
				v.logger.Errorf("Error creating machine: %s", err.Error())
			}

			if err := v.machine.Start(v.vmmCtx); err != nil {
				v.logger.Errorf("Error starting virtual machine: %s", err.Error())
			}
			v.state = virtualizers.Alive

			go func() {
				v.routes = util.LookForIP(v.serialLogger, v.routes)
			}()

			if err := v.machine.Wait(v.vmmCtx); err != nil {
				// Should end when we ctrl-c no need to print this.
				if !strings.Contains(err.Error(), "* signal: interrupt") {
					v.logger.Errorf("Wait returned an error: %s", err.Error())
				}
			}
			v.state = virtualizers.Ready

		}()
	}
	return nil
}
