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
	"time"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	dhcp "github.com/krolaw/dhcp4"
	conn "github.com/krolaw/dhcp4/conn"
	"github.com/milosgajdos/tenus"
	log "github.com/sirupsen/logrus"
	"github.com/songgao/water"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	dhcpHandler "github.com/vorteil/vorteil/pkg/virtualizers/dhcp"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
)

func FetchBridgeDev() error {
	// Check if bridge device exists
	_, err := tenus.BridgeFromName("vorteil-bridge")
	if err != nil {
		return errors.New("try running 'vorteil firecracker-setup' before using firecracker")
	}
	return err
}
func SetupBridgeAndDHCPServer() error {

	// Create bridge device
	bridger, err := tenus.NewBridgeWithName("vorteil-bridge")
	if err != nil {
		if !strings.Contains(err.Error(), "Interface name vorteil-bridge already assigned on the host") {
			return err
		}
		// get bridge device
		bridger, err = tenus.BridgeFromName("vorteil-bridge")
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
	// create dhcp server on an interface
	server := dhcpHandler.NewHandler()
	pc, err := conn.NewUDP4BoundListener("vorteil-bridge", ":67")
	if err != nil {
		return err
	}

	// create server handler to create tap devices under sudo
	http.HandleFunc("/", OrganiseTapDevices)
	go func() {
		fmt.Println("listenandserve")
		http.ListenAndServe(":7476", nil)
	}()
	// Start dhcp server to listen
	dhcp.Serve(pc, server)

	return nil
}

type CreateDevices struct {
	id     string                          `json:"id"`
	routes []virtualizers.NetworkInterface `json:"routes"`
}

type Devices struct {
	devices []string `json:"devices"`
}

func OrganiseTapDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		fmt.Printf("POST")
		var cd CreateDevices
		var tapDevices []string

		err := json.NewDecoder(r.Body).Decode(cd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		fmt.Printf("CD: %v", cd)

		// get bridge device
		bridgeDev, err := tenus.BridgeFromName("vorteil-bridge")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

		}

		// set network adapters
		if len(cd.routes) > 0 {
			for i := 0; i < len(cd.routes); i++ {
				ifceName := fmt.Sprintf("%s-%s", cd.id, strconv.Itoa(i))

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
				devices: tapDevices,
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

	default:
		http.Error(w, "method not available", http.StatusBadRequest)
	}
}

// DownloadPath is the path where we pull firecracker-vmlinux's from
const DownloadPath = "https://storage.googleapis.com/vorteil-dl/firecracker-vmlinux/"

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
	logger       elog.Logger    // logger
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

	bridgeDevice tenus.Bridger  // bridge device e.g vorteil-bridge
	tapDevice    []tenus.Linker // tap device for the machine

	vmdrive string // store disks in this directory
}

// Detach ... Potentially Todo i think firecracker detach is alot more complicated because of the tap devices
func (v *Virtualizer) Detach(source string) error {
	if v.state != virtualizers.Ready {
		return errors.New("virtual machine must be in ready state to detach")
	}

	name := filepath.Base(v.folder)

	err := os.MkdirAll(filepath.Join(source), 0777)
	if err != nil {
		return err
	}
	err = os.Rename(v.folder, filepath.Join(source, name))
	if err != nil {
		return err
	}

	v.config.VM.RAM.Align(vcfg.MiB * 2)

	// close and cleanup tap devices
	// stopVMM
	err = v.machine.StopVMM()
	if err != nil {
		return err
	}

	// sleep for shutdown signal
	time.Sleep(time.Second * 4)
	// delete tap device as vmm has been stopped don't worry about catching error as its not found
	for _, device := range v.tapDevice {
		device.DeleteLink()
	}

	v.state = virtualizers.Deleted

	cleanup, err := os.Create(filepath.Join(source, name, "cleanup.sh"))
	if err != nil {
		return err
	}
	defer cleanup.Close()
	var tapArgs []string
	var tapCreation []string
	type NetworkInterface struct {
		IfaceID     string `json:"iface_id"`
		HostDevName string `json:"host_dev_name"`
	}
	var networkCards []NetworkInterface
	// write script for Tap setup
	if len(v.routes) > 0 {
		for i := 0; i < len(v.routes); i++ {
			ifceName := fmt.Sprintf("%s-%s", v.id, strconv.Itoa(i))
			tapCreation = append(tapCreation, ifceName)
		}
		for _, tap := range tapCreation {
			tapArgs = append(tapArgs, fmt.Sprintf("sudo ip tuntap add dev %s mode tap", tap))
			tapArgs = append(tapArgs, fmt.Sprintf("sudo brctl addif vorteil-bridge %s", tap))
			tapArgs = append(tapArgs, fmt.Sprintf("sudo ip link set dev %s up", tap))
			cleanup.Write([]byte(fmt.Sprintf("sudo ip link delete %s\n", tap)))

			networkCards = append(networkCards, NetworkInterface{
				IfaceID:     "vorteil-bridge",
				HostDevName: tap,
			})
		}
	}
	f, err := os.Create(filepath.Join(source, name, "interfaces.sh"))
	if err != nil {
		return err
	}
	f.Write([]byte(strings.Join(tapArgs, "\n")))
	defer f.Close()

	type Drive struct {
		DriveID      string `json:"drive_id"`
		PathOnHost   string `json:"path_on_host"`
		IsRootDevice bool   `json:"is_root_device"`
		IsReadOnly   bool   `json:"is_read_only"`
	}
	type BootSource struct {
		KernelImagePath string `json:"kernel_image_path"`
		BootArgs        string `json:"boot_args"`
	}
	type MachineConfig struct {
		VcpuCount  int  `json:"vcpu_count"`
		MemSizeMib int  `json:"mem_size_mib"`
		HtEnabled  bool `json:"ht_enabled"`
	}
	type fcConfig struct {
		BootSource        BootSource         `json:"boot-source"`
		Drives            []Drive            `json:"drives"`
		MachineConfig     MachineConfig      `json:"machine-config"`
		NetworkInterfaces []NetworkInterface `json:"network-interfaces"`
	}

	drive := Drive{
		DriveID:      "rootfs",
		PathOnHost:   filepath.Join(source, name, fmt.Sprintf("%s.raw", v.name)),
		IsRootDevice: true,
		IsReadOnly:   false,
	}
	var drives []Drive
	drives = append(drives, drive)

	var config fcConfig
	config.Drives = drives
	config.BootSource = BootSource{
		KernelImagePath: v.kip,
		BootArgs:        "init=/vorteil/vinitd reboot=k panic=1 pci=off i8042.noaux i8042.nomux i8042.nopnp i8042.dumbkbd  vt.color=0x00",
	}
	config.MachineConfig = MachineConfig{
		VcpuCount:  int(v.config.VM.CPUs),
		HtEnabled:  false,
		MemSizeMib: v.config.VM.RAM.Units(vcfg.MiB),
	}
	config.NetworkInterfaces = networkCards

	data, err := json.Marshal(config)
	if err != nil {
		return err
	}

	jf, err := os.Create(filepath.Join(source, name, "config.json"))
	if err != nil {
		return err
	}
	defer jf.Close()
	jf.Write(data)

	start, err := os.Create(filepath.Join(source, name, "start.sh"))
	if err != nil {
		return err
	}
	defer start.Close()
	start.Write([]byte("sudo ./interfaces.sh\nfirecracker --api-sock ./firecracker.socket --config-file ./config.json"))

	// Chmod scripts
	err = os.Chmod(start.Name(), 0777)
	if err != nil {
		return err
	}
	err = os.Chmod(f.Name(), 0777)
	if err != nil {
		return err
	}
	err = os.Chmod(cleanup.Name(), 0777)
	if err != nil {
		return err
	}

	// remove virtualizer from active vms
	virtualizers.ActiveVMs.Delete(v.name)
	return nil
}

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
		// pipe stream
		body := io.TeeReader(resp.Body, newWriter(int64(length), func(downloaded, total int64) {
			o.updateStatus(fmt.Sprintf("Downloading VMLinux(%s/%s)", ByteCountDecimal(downloaded), ByteCountDecimal(total)))
		}))
		_, err = io.Copy(file, body)
		if err != nil {
			os.Remove(file.Name())
			return "", err
		}
	}

	return filepath.Join(o.firecrackerPath, kernel), nil
}

// ByteCountDecimal converts bytes to readable format
func ByteCountDecimal(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
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
	o.logger.Errorf("Error: %v", err)
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
	v.logger.Debugf("Stopping VM")
	if v.state != virtualizers.Ready {
		v.state = virtualizers.Changing

		err := v.machine.Shutdown(v.vmmCtx)
		if err != nil {
			return err
		}
		// wait for shutdown don't think theres a better way other than a sleep

		v.state = virtualizers.Ready

	} else {
		return errors.New("vm is already stopped")
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

// Close shuts down the virutal machien and cleans up the disk and folders
func (v *Virtualizer) Close(force bool) error {
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

	// sleep for shutdown signal
	// time.Sleep(time.Second * 4)
	// delete tap device as vmm has been stopped don't worry about catching error as its not found
	for _, device := range v.tapDevice {
		device.DeleteLink()
	}
	// v.tapDevice.DeleteLink()

	v.state = virtualizers.Deleted

	// remove virtualizer from active vms
	virtualizers.ActiveVMs.Delete(v.name)

	// remove contents when closing
	err = os.RemoveAll(v.folder)
	if err != nil {
		return err
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
	v.routes = v.Routes()

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

// lookForIp looks for IP via the screen output as firecracker spawns on different IPs
func (v *Virtualizer) lookForIP() string {
	sub := v.serialLogger.Subscribe()
	inbox := sub.Inbox()
	var msg string
	timer := false
	msgWrote := false
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case logdata, _ := <-inbox:
			msg += string(logdata)
			if strings.TrimSpace(msg) != "" && strings.Contains(msg, "ip") {
				msgWrote = true
			}
		case <-ticker.C:
			if msgWrote {
				// sleep slightly so we get all the IPS
				time.Sleep(time.Second * 1)
				timer = true
			}
		// after 30 seconds break out of for loop for memory resolving
		case <-time.After(time.Second * 30):
			timer = true
		}
		if timer {
			break
		}
	}
	var ips []string
	lines := strings.Split(msg, "\r\n")
	for _, line := range lines {
		if virtualizers.IPRegex.MatchString(line) {
			if strings.Contains(line, "ip") {
				split := strings.Split(line, ":")
				if len(split) > 1 {
					ips = append(ips, strings.TrimSpace(split[1]))
				}
			}
		}
	}

	if len(ips) > 0 {
		for i, route := range v.routes {
			for j, port := range route.HTTP {
				v.routes[i].HTTP[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
			}
			for j, port := range route.HTTPS {
				v.routes[i].HTTPS[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
			}
			for j, port := range route.TCP {
				v.routes[i].TCP[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
			}
			for j, port := range route.UDP {
				v.routes[i].UDP[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
			}
		}
		return ips[0]
	}
	return ""
}

// Write method to handle logging from firecracker to use our logger interface
// Cant use logger interface as it duplicates
func (v *Virtualizer) Write(d []byte) (n int, err error) {
	n = len(d)
	fmt.Print(string(d))
	return
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
	logger.SetFormatter(&log.TextFormatter{
		DisableColors: false,
		ForceColors:   true,
	})
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

	o.log("debug", "Fetching VMLinux from cache or online")
	o.kip, err = o.fetchVMLinux(o.config.VM.Kernel)
	if err != nil {
		returnErr = err
		return
	}
	o.log("debug", "Finished getting VMLinux")

	cd := &CreateDevices{
		id:     o.id,
		routes: o.routes,
	}

	cdm, err := json.Marshal(cd)
	if err != nil {
		fmt.Printf("ER")
		returnErr = err
		return
	}
	fmt.Printf("SENDING: %s\n", string(cdm))
	resp, err := http.Post("http://localhost:7476/", "application/json", bytes.NewBuffer(cdm))
	if err != nil {
		fmt.Printf("ER2")

		returnErr = err
		return
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("ER3")

		returnErr = err
		return
	}
	fmt.Printf("BODY:%s\n", string(body))
	var ifs Devices
	err = json.Unmarshal(body, ifs)
	if err != nil {
		fmt.Printf("ER4")

		returnErr = err
		return
	}

	// TODO this needs to move into where the DHCP handler is
	var interfaces []firecracker.NetworkInterface

	for i := 0; i < len(ifs.devices); i++ {
		interfaces = append(interfaces,
			firecracker.NetworkInterface{
				StaticConfiguration: &firecracker.StaticNetworkConfiguration{
					HostDevName: ifs.devices[i],
				},
			},
		)
	}

	//END TODO

	fcCfg := firecracker.Config{
		SocketPath:      filepath.Join(o.folder, fmt.Sprintf("%s.%s", o.name, "socket")),
		KernelImagePath: o.kip,
		KernelArgs:      fmt.Sprintf("init=/vorteil/vinitd root=PARTUUID=%s loglevel=9 reboot=k panic=1 pci=off i8042.noaux i8042.nomux i8042.nopnp i8042.dumbkbd  vt.color=0x00", vimg.Part2UUIDString),
		Drives:          devices,
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(int64(o.config.VM.CPUs)),
			HtEnabled:  firecracker.Bool(false),
			MemSizeMib: firecracker.Int64(int64(o.config.VM.RAM.Units(vcfg.MiB))),
		},
		NetworkInterfaces: interfaces,
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
			v.machineOpts = append(v.machineOpts, firecracker.WithProcessRunner(cmd))

			v.machine, err = firecracker.NewMachine(v.vmmCtx, v.fconfig, v.machineOpts...)
			if err != nil {
				v.logger.Errorf("Error creating machine: %s", err.Error())
			}

			if err := v.machine.Start(v.vmmCtx); err != nil {
				v.logger.Errorf("Error starting virtual machine: %s", err.Error())
			}
			v.state = virtualizers.Alive

			go v.lookForIP()

			if err := v.machine.Wait(v.vmmCtx); err != nil {
				v.logger.Errorf("Wait returned an error: %s", err.Error())
			}
		}()
	}
	return nil
}

// Routes converts the VCFG.routes to the apiNetworkInterface which allows
// us to easiler return to currently written graphql APIs
func (v *Virtualizer) Routes() []virtualizers.NetworkInterface {

	routes := virtualizers.Routes{}
	var nics = v.config.Networks
	for i, nic := range nics {
		if nic.IP == "" {
			continue
		}
		protocols := []string{
			"udp",
			"tcp",
			"http",
			"https",
		}
		portLists := [][]string{
			nic.UDP,
			nic.TCP,
			nic.HTTP,
			nic.HTTPS,
		}
		for j := 0; j < len(protocols); j++ {
			protocol := protocols[j]
			ports := portLists[j]
			if routes.NIC[i].Protocol == nil {
				routes.NIC[i].Protocol = make(map[virtualizers.NetworkProtocol]*virtualizers.NetworkProtocolPorts)
			}
			if protocol == "" {
				protocol = "http"
			}
			p := virtualizers.NetworkProtocol(protocol)
			existingPorts, ok := routes.NIC[i].Protocol[p]
			if !ok {
				existingPorts = &virtualizers.NetworkProtocolPorts{
					Port: make(map[string]*virtualizers.NetworkRoute),
				}
			}
			for _, port := range ports {
				existingPorts.Port[port] = new(virtualizers.NetworkRoute)
			}
			routes.NIC[i].Protocol[p] = existingPorts
		}
	}
	apiNics := make([]virtualizers.NetworkInterface, 0)
	for i, net := range v.config.Networks {
		newNetwork := virtualizers.NetworkInterface{
			Name:    "",
			IP:      net.IP,
			Mask:    net.Mask,
			Gateway: net.Gateway,
		}
		for _, port := range net.UDP {
			var addr string
			if len(routes.NIC) > i {
				nic := routes.NIC[i]
				if proto, ok := nic.Protocol["udp"]; ok {
					if pmap, ok := proto.Port[port]; ok {
						addr = pmap.Address
					}
				}
			}
			newNetwork.UDP = append(newNetwork.UDP, virtualizers.RouteMap{
				Port:    port,
				Address: addr,
			})
		}
		for _, port := range net.TCP {
			var addr string
			if len(routes.NIC) > i {
				nic := routes.NIC[i]
				if proto, ok := nic.Protocol["tcp"]; ok {
					if pmap, ok := proto.Port[port]; ok {
						addr = pmap.Address
					}
				}
			}
			newNetwork.TCP = append(newNetwork.TCP, virtualizers.RouteMap{
				Port:    port,
				Address: addr,
			})
		}
		for _, port := range net.HTTP {
			var addr string
			if len(routes.NIC) > i {
				nic := routes.NIC[i]
				if proto, ok := nic.Protocol["http"]; ok {
					if pmap, ok := proto.Port[port]; ok {
						addr = pmap.Address
					}
				}
			}
			newNetwork.HTTP = append(newNetwork.HTTP, virtualizers.RouteMap{
				Port:    port,
				Address: addr,
			})
		}
		for _, port := range net.HTTPS {
			var addr string
			if len(routes.NIC) > i {
				nic := routes.NIC[i]
				if proto, ok := nic.Protocol["https"]; ok {
					if pmap, ok := proto.Port[port]; ok {
						addr = pmap.Address
					}
				}
			}
			newNetwork.HTTPS = append(newNetwork.HTTPS, virtualizers.RouteMap{
				Port:    port,
				Address: addr,
			})
		}
		apiNics = append(apiNics, newNetwork)
	}
	return apiNics
}
