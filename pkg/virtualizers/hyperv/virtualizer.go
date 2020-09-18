package hyperv

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
)

// Virtualizer is a struct which will implement the interface so the manager can create one
type Virtualizer struct {
	id           string      // rando has for named pipes and folder names
	name         string      // name of vm
	pname        string      // name of virtualizer
	source       interface{} // details on how the virtual machine was made
	state        string      // the state of the vm
	headless     bool        // whether to show a gui when spawning a vm
	created      time.Time   // time the vm was created
	switchName   string      // The virtual switch hyper-v will use
	folder       string      // The folder to store vm details and objects
	disk         *os.File    // disk of the machine
	logger       elog.Logger
	serialLogger *logger.Logger                  // logs for the serial of the vm
	routes       []virtualizers.NetworkInterface // api network interface that displays ports and network types

	config  *vcfg.VCFG // config for the vm
	sock    net.Conn   // net connection to listen for serial on
	vmdrive string     // store disks in this directory

}

// execute is a general function for running commands through powershell
func (v *Virtualizer) execute(cmd *exec.Cmd) (string, error) {
	if !strings.Contains(strings.Join(cmd.Args, " "), "| Select State") {
		v.logger.Infof("Executing %s", cmd.Args)
	}
	resp, err := cmd.CombinedOutput()
	if err != nil {
		if err.Error() == "" {
			return "", errors.New(string(resp))
		}
	}
	output := string(resp)
	return output, nil
}

// Type returns the type of virtualizer
func (v *Virtualizer) Type() string {
	return VirtualizerID
}

// State returns the state of the virtualizer
func (v *Virtualizer) State() string {
	return v.state
}

// Stop stops the vm and changes the status back to 'ready'
func (v *Virtualizer) Stop() error {
	v.logger.Debugf("Stopping VM")
	if v.state != virtualizers.Ready {
		v.state = virtualizers.Changing

		go func() {
			time.Sleep(time.Second * 12)
			state, err := v.getState()
			if err != nil {
				v.logger.Errorf("Getting State: %s", err)
			}
			// remove vm as unable to stop within 10 seconds
			if state == "Running" {
				cmd := exec.Command(virtualizers.Powershell, "Remove-VM", "-Name", v.name, "-Force")
				_, err := v.execute(cmd)
				if err != nil {
					v.logger.Errorf("Error Remove-VM: %v", err)
				}
			}
		}()

		cmd := exec.Command(virtualizers.Powershell, "Stop-VM", "-Name", v.name)
		output, err := v.execute(cmd)
		if err != nil {
			v.logger.Errorf("Error Stop-VM: %v", err)
			return err
		}
		if len(output) != 0 {
			v.logger.Infof("%s", output)
		}

		v.state = virtualizers.Ready
	}
	return nil
}

// Start creates the virtualmachine and runs it
func (v *Virtualizer) Start() error {
	v.logger.Debugf("Starting VM")
	switch v.State() {
	case "ready":
		v.state = virtualizers.Changing

		cmd := exec.Command(virtualizers.Powershell, "Start-VM", "-Name", v.name)
		output, err := v.execute(cmd)
		if err != nil {
			v.logger.Errorf("Error Start-VM: %v", err)
			return err
		}

		if len(output) != 0 {
			v.logger.Infof("%s", output)
		}

		err = v.initLogs()
		if err != nil {
			return err
		}

		v.state = virtualizers.Alive

		go v.checkState()
		go v.lookForIP()
		// go v.checkVMList()

		if !v.headless {
			vmconnect, err := exec.LookPath("vmconnect.exe")
			if err != nil {
				return fmt.Errorf("Error finding vmconnect: %v", err)
			}
			// goroutine this as it hangs the processes forking a program on windows to open connection with vm
			go func() {
				cmd = exec.Command(vmconnect, "localhost", v.name)
				_, err := v.execute(cmd)
				if err != nil {
					v.logger.Errorf("Error VMConnect: %v", err)
				}
			}()
		}
	default:
		return fmt.Errorf("vm not in a state to be started currently in: %s", v.State())
	}
	return nil
}

// lookForIp is a function that screen reads the logs to get the ip address the app is spawned on
func (v *Virtualizer) lookForIP() {
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
	}

}

// Download returns the disk as a vio.File
func (v *Virtualizer) Download() (vio.File, error) {
	if !(v.state == virtualizers.Ready) {
		return nil, fmt.Errorf("virtual machine must be in state ready to be downloaded")
	}
	f, err := vio.LazyOpen(v.disk.Name())
	if err != nil {
		v.logger.Errorf("Error Downloading Disk Image: %s", err.Error())
		return nil, err
	}
	return f, nil
}

// ConvertToVM is a wrapper function that provides us to use the old APIs
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
		URL:      string(info.URL),
		Version:  info.Version,
		Programs: programs,
		// Source:   v.source.(virtualizers.Source),
		Hostname: system.Hostname,
		Platform: v.pname,
		Status:   v.state,
	}

	return machine
}

// Close shuts down the virtual machine and cleans up the disk and folders
func (v *Virtualizer) Close(force bool) error {
	v.logger.Debugf("Deleting VM")
	// if !(force) {
	if !(v.state == virtualizers.Ready) {
		err := v.Stop()
		if err != nil {
			if !strings.Contains(err.Error(), "not currently running") {
				v.logger.Errorf("Error Stopping VM: %v", err)
				return err
			}
		}
	}
	// }
	v.state = virtualizers.Changing

	cmd := exec.Command(virtualizers.Powershell, "Remove-VM", "-Name", v.name, "-Force")
	_, err := v.execute(cmd)
	if err != nil {
		v.logger.Errorf("Error Remove-VM: %v", err)
	}
	v.state = virtualizers.Deleted

	v.disk.Close()
	if v.sock != nil {
		v.sock.Close()
	}
	virtualizers.ActiveVMs.Delete(v.name)
	err = os.RemoveAll(v.folder)
	if err != nil {
		return err
	}

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
	v.switchName = c.SwitchName
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

// getState gets the state of the vm to maintain whether or not its still alive
func (v *Virtualizer) getState() (string, error) {
	cmd := exec.Command(virtualizers.Powershell, "Get-VM", "-Name", v.name, "|", "Select", "State")
	output, err := v.execute(cmd)
	if err != nil {
		v.logger.Errorf("Error Get-VM: %v", err)
		return "", err
	}
	if len(output) != 0 {
		readStr := strings.Split(output, "-----")
		if len(readStr) >= 2 {
			return strings.TrimSpace(readStr[1]), nil
		}
	}

	if strings.Contains(output, "Hyper-V was unable to find a virtual machine with name") {
		err = v.Close(true)
		if err != nil {
			v.logger.Errorf("Error closing VM as it doesn't exist: %v", err)
		}
		return "", errors.New("machine doesn't exist on hyper-v anymore")
	}
	return "", nil
}

// checkState is a one second loop that calls get state to read the vm status
// So we can run accordingly
func (v *Virtualizer) checkState() {
	for {
		state, err := v.getState()
		if err != nil {
			v.logger.Errorf("Getting State: %s", err)
			break
		}
		if state == "Off" {
			v.state = virtualizers.Ready

		}
		time.Sleep(time.Second * 1)
	}
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

func (v *Virtualizer) GeneratePowershell(source string) error {
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

	size := fmt.Sprintf("%v%s", v.config.VM.RAM.Units(vcfg.MiB), "MB")

	var args []string
	// New-VM
	args = append(args, fmt.Sprintf("New-VM -Name %s -BootDevice VHD -VHDPath %s -Path %s -Generation 1 -SwitchName \"%s\"",
		v.name, filepath.Join(source, name, fmt.Sprintf("disk.vhd")), filepath.Join(source, name), v.switchName))

	// Set-VMMemory
	args = append(args, fmt.Sprintf("Set-VMMemory -VMName %s -DynamicMemoryEnabled 0 -Startupbytes %s", v.name, size))

	// Add-VMNetworkAdapter
	if len(v.routes) > 1 {
		for i := 1; i < len(v.routes); i++ {
			args = append(args, fmt.Sprintf("Add-VMNetworkAdapter -VMName %s -SwitchName \"%s\"", v.name, v.switchName))
		}
	}

	// Set-VMProcessor
	args = append(args, fmt.Sprintf("Set-VMProcessor -VMName %s -Count %s -ExposeVirtualizationExtensions $true", v.name, strconv.Itoa(int(v.config.VM.CPUs))))

	// Start-VM
	args = append(args, fmt.Sprintf("Start-VM -Name %s", v.name))

	// vmconnect gui to go with command
	vmconnect, err := exec.LookPath("vmconnect.exe")
	if err != nil {
		return fmt.Errorf("error finding vmconnect: %v", err)
	}
	args = append(args, fmt.Sprintf("%s localhost %s", vmconnect, v.name))

	// Create stop script
	stop, err := os.Create(filepath.Join(source, name, "stop.ps1"))
	if err != nil {
		return err
	}

	// Stop-VM
	stop.Write([]byte(fmt.Sprintf("Stop-VM -Name %s\n", v.name)))

	// Remove-VM
	stop.Write([]byte(fmt.Sprintf("Remove-VM -Name %s -Force", v.name)))

	// Create start script
	f, err := os.Create(filepath.Join(source, name, "start.ps1"))
	if err != nil {
		return err
	}

	f.Write([]byte(strings.Join(args, "\n")))
	defer f.Close()
	defer stop.Close()
	return nil
}

// Detach removes vm from active vm list
func (v *Virtualizer) Detach(source string) error {
	if v.state != virtualizers.Ready {
		return errors.New("virtual machine must be in a ready state to detach")
	}

	// clean up from hyper-v
	cmd := exec.Command(virtualizers.Powershell, "Remove-VM", "-Name", v.name, "-Force")
	_, err := v.execute(cmd)
	if err != nil {
		v.logger.Errorf("Error Remove-VM: %v", err)
	}
	v.state = virtualizers.Deleted

	v.disk.Close()
	if v.sock != nil {

		v.sock.Close()
	}

	err = v.GeneratePowershell(source)
	if err != nil {
		return err
	}

	virtualizers.ActiveVMs.Delete(v.name)

	return nil
}

// Prepare prepares the virtualizer with the appropriate fields to run the virtual machine
func (v *Virtualizer) Prepare(args *virtualizers.PrepareArgs) *virtualizers.VirtualizeOperation {

	op := new(operation)
	op.Virtualizer = v
	v.name = args.Name
	v.config = args.Config
	v.pname = args.PName
	// v.source = args.Source
	v.routes = v.Routes()
	v.state = virtualizers.Changing
	v.vmdrive = args.VMDrive
	v.created = time.Now()
	v.logger = args.Logger
	v.serialLogger = logger.NewLogger(2048 * 10)
	v.logger.Debugf("Preparing VM")

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

// prepare sets the fields and arguments to spawn the virtual machine
func (o *operation) prepare(args *virtualizers.PrepareArgs) {
	var returnErr error

	o.updateStatus(fmt.Sprintf("Preparing hyperv files..."))
	defer func() {
		if returnErr != nil {
			o.logger.Errorf("Error Preparing VM: %v", returnErr)
		}
		o.finished(returnErr)
	}()
	o.name = args.Name
	o.folder = filepath.Dir(args.ImagePath)
	o.id = strings.Split(filepath.Base(o.folder), "-")[1]

	_, loaded := virtualizers.ActiveVMs.LoadOrStore(o.name, o.Virtualizer)
	if loaded {
		returnErr = errors.New("virtual machine already exists")
	}

	o.config.VM.RAM.Align(vcfg.MiB * 2)

	size := fmt.Sprintf("%v%s", o.config.VM.RAM.Units(vcfg.MiB), "MB")

	cmd := exec.Command(virtualizers.Powershell, "New-VM", "-Name", o.name,
		"-BootDevice", "VHD", "-VHDPath", filepath.ToSlash(args.ImagePath), "-Path", o.folder, "-Generation", "1", "-SwitchName", fmt.Sprintf("\"%s\"", o.switchName))
	output, err := o.execute(cmd)
	if err != nil {
		o.logger.Errorf("Error New-VM: %v", err)
	}
	if len(output) != 0 {
		o.logger.Errorf("%s", output)
	}

	// set vm memory
	cmd = exec.Command(virtualizers.Powershell, "Set-VMMemory", "-VMName", o.name, "-DynamicMemoryEnabled", "0", "-StartupBytes", size)
	output, err = o.execute(cmd)
	if err != nil {
		o.logger.Errorf("Error Set-VMMemory: %v", err)
	}
	if len(output) != 0 {
		o.logger.Infof("%s", output)
	}

	// set network adapters
	if len(o.routes) > 1 {
		for i := 1; i < len(o.routes); i++ {
			cmd = exec.Command(virtualizers.Powershell, "Add-VMNetworkAdapter", "-VMName", o.name, "-SwitchName", fmt.Sprintf("\"%s\"", o.switchName))
			output, err = o.execute(cmd)
			if err != nil {
				o.logger.Errorf("Error Adding VMNetwork Adapter: %v", err)
			}
			if len(output) != 0 {
				o.logger.Infof("%s", output)
			}
		}
	}
	// set vm processors
	cmd = exec.Command(virtualizers.Powershell, "Set-VMProcessor", "-VMName", o.name, "-Count", strconv.Itoa(int(o.config.VM.CPUs)), "-ExposeVirtualizationExtensions", "$true")
	output, err = o.execute(cmd)
	if err != nil {
		o.logger.Errorf("Error Set-VMProcessor: %v", err)
	}
	if len(output) != 0 {
		o.logger.Infof("%s", output)
	}

	pipePath := fmt.Sprintf("\\\\.\\pipe\\%s", o.id)

	cmd = exec.Command(virtualizers.Powershell, "Set-VMComPort", "-VMName", o.name, "-Path", fmt.Sprintf("\"%s\"", pipePath), "-Number", "1")
	output, err = o.execute(cmd)
	if err != nil {
		o.logger.Errorf("error", "Error Set-VMComPort: %v", err)
	}
	if len(output) != 0 {
		o.logger.Infof("info", "%s", output)
	}

	o.state = "ready"

	if args.Start {
		err = o.Start()
		if err != nil {
			returnErr = err
		}
	}

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
