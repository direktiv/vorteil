package qemu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-shellwords"
	"github.com/sisatech/goapi/pkg/file"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
)

// osFlags are flags qemu required that differ for every operating system
var osFlags = "-cpu host -enable-kvm"

// change os flags depending on what operating system its being run on
func init() {
	switch runtime.GOOS {
	case "windows":
		osFlags = "-accel whpx"
	case "darwin":
		osFlags = "-accel hvf"
	}
}

// Virtualizer is a struct which will implement the interface so the manager can create VMs
type Virtualizer struct {
	// VM related stuff
	id          string      // rando hash for named pipes and folder names
	name        string      // name of vm
	pname       string      // name of virtualizer
	state       string      // status of vm
	headless    bool        // to display gui or not
	created     time.Time   // time the vm was created
	networkType string      // type of network to spawn on
	folder      string      // folder to store vm details
	disk        *os.File    // disk of the machine
	source      interface{} // details about how the vm was made
	// loggers
	virtLogger   *logger.Logger // logs about the provisioning process
	serialLogger *logger.Logger // logs for the serial of the vm
	// QEMU Specific
	command *exec.Cmd     // The execute command to start the qemu instance
	errPipe io.ReadCloser // Stderr for this Virtual Machine
	outPipe io.ReadCloser // Stdout for this Virtual Machine
	sock    net.Conn      // net connection

	// VCFG Stuff
	routes []virtualizers.NetworkInterface // api network interface that displays ports and network types
	config *vcfg.VCFG                      // config for the vm
	// subServer *graph.Graph                    //

	vmdrive string // store disks in this directory

}

// createArgs create generic qemu arguments for running a VM on QEMU
func createArgs(cpus uint, memory int, headless bool, diskpath string, diskformat string) string {
	argsCommand := fmt.Sprintf("%s -no-reboot -machine q35 -smp %v -m %v -serial stdio", osFlags, cpus, memory)

	if headless {
		argsCommand += fmt.Sprintf(" -display none")
	} else if runtime.GOOS != "darwin" {
		// darwin by default opens a gui window
		argsCommand += fmt.Sprintf(" -display gtk")
	}

	argsCommand += fmt.Sprintf(" -device virtio-scsi-pci,id=scsi -device scsi-hd,drive=hd0 -drive if=none,file=\"%s\",format=%s,id=hd0", diskpath, diskformat)

	return argsCommand
}

// log writes a log line to the logger and adds prefix and suffix depending on what type of log was sent.
func (v *Virtualizer) log(logType string, text string, args ...interface{}) {
	switch logType {
	case "error":
		text = fmt.Sprintf("%s%s%s\n", "\033[31m", text, "\033[0m")
	case "warning":
		text = fmt.Sprintf("%s%s%s\n", "\033[33m", text, "\033[0m")
	case "info":
		text = fmt.Sprintf("%s%s%s\n", "\u001b[37;1m", text, "\u001b[0m")
	default:
		text = fmt.Sprintf("%s\n", text)
	}
	v.virtLogger.Write([]byte(fmt.Sprintf(text, args...)))
}

// Type returns the type of virtualizer
func (v *Virtualizer) Type() string {
	return VirtualizerID
}

// Initialize passes the arguments from creation to create a virtualizer
func (v *Virtualizer) Initialize(data []byte) error {
	c := new(Config)
	err := c.Unmarshal(data)
	if err != nil {
		return err
	}

	v.headless = c.Headless
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

// Logs returns virtualizer logs. Shows what to execute
func (v *Virtualizer) Logs() *logger.Logger {
	return v.virtLogger
}

// Serial returns the serial logger which contains the serial output of the app.
func (v *Virtualizer) Serial() *logger.Logger {
	return v.serialLogger
}

// ForceStop is the same as stop without the sleep so we get no logs and the disk is freed to be deleted quicker.
func (v *Virtualizer) ForceStop() error {
	v.log("debug", "Stopping VM")
	if v.state != virtualizers.Ready {
		v.state = virtualizers.Changing
		// v.subServer.SubServer.Publish(graph.VMUpdater)

		if v.sock != nil {
			if runtime.GOOS != "windows" {
				defer os.RemoveAll(filepath.ToSlash(filepath.Join(v.folder, "monitor.sock")))
			}
			_, err := v.sock.Write([]byte("system_reset\n"))
			if err != nil && err.Error() != fmt.Errorf("The pipe is being closed.").Error() && err.Error() != fmt.Errorf("write unix @->%s: write: broken pipe", filepath.ToSlash(filepath.Join(v.folder, "monitor.sock"))).Error() {
				v.log("error", "Error system_powerdown: %v", err)
				return err
			}

			time.Sleep(time.Millisecond * 200)
			v.state = virtualizers.Ready

			// v.subServer.SubServer.Publish(graph.VMUpdater)

			v.sock.Close()

			// vm should be stopped by now so close the pipes
			v.errPipe.Close()
			v.outPipe.Close()
			v.disk.Close()
		}
	} else {
		return errors.New("vm is already stopped.")
	}

	return nil
}

// Stop stops the vm and changes the status back to 'ready'
func (v *Virtualizer) Stop() error {
	v.log("debug", "Stopping VM")
	if v.state != virtualizers.Ready {
		v.state = virtualizers.Changing

		if v.sock != nil {
			if runtime.GOOS != "windows" {
				defer os.RemoveAll(filepath.ToSlash(filepath.Join(v.folder, "monitor.sock")))
			}
			_, err := v.sock.Write([]byte("system_powerdown\n"))
			if err != nil && err.Error() != fmt.Errorf("The pipe is being closed.").Error() && err.Error() != fmt.Errorf("write unix @->%s: write: broken pipe", filepath.ToSlash(filepath.Join(v.folder, "monitor.sock"))).Error() {
				v.log("error", "Error system_powerdown: %v", err)
				return err
			}

			// Sleep is to log the entire shutdown sequence
			time.Sleep(time.Second * 4)

			v.state = virtualizers.Ready
			// v.subServer.SubServer.Publish(graph.VMUpdater)

			v.sock.Close()

			// vm should be stopped by now so close the pipes
			v.errPipe.Close()
			v.outPipe.Close()
			v.disk.Close()
		}
	} else {
		return errors.New("vm is already stopped.")
	}

	return nil
}

// initLogging pipes the command err and out pipes to the serial loggers
func (v *Virtualizer) initLogging() error {
	var err error
	v.errPipe, err = v.command.StderrPipe()
	if err != nil {
		v.log("error", "Error setting errPipe for command: %v", err)
		return err
	}
	v.outPipe, err = v.command.StdoutPipe()
	if err != nil {
		v.log("error", "Error setting outPipe for command: %v", err)
		return err
	}

	go io.Copy(v.serialLogger, v.outPipe)
	go io.Copy(v.serialLogger, v.errPipe)

	return nil
}

// initializeNetworkCards attempts the bind the ports provided and if it fails attempts to bind to a random port and adds it to the arguments for the qemu command.
func (v *Virtualizer) initializeNetworkCards() ([]string, error) {
	v.log("debug", "Initializing Network Cards")

	var nicArgs string
	noNic := 0
	hasDefinedPorts := false

	for i, route := range v.routes {
		var args string
		noNic++
		protocol := "tcp"

		for j, port := range route.HTTP {
			bind, nr, err := virtualizers.BindPort(v.networkType, protocol, port.Port)
			if err != nil {
				v.log("error", "Error binding port: %v", err)
				return nil, err
			}
			v.routes[i].HTTP[j].Address = nr
			hasDefinedPorts = true
			args += fmt.Sprintf(",hostfwd=%s::%s-:%s", protocol, bind, port.Port)
		}
		for j, port := range route.HTTPS {
			bind, nr, err := virtualizers.BindPort(v.networkType, protocol, port.Port)
			if err != nil {
				v.log("error", "Error binding port: %v", err)
				return nil, err
			}
			v.routes[i].HTTPS[j].Address = nr
			hasDefinedPorts = true
			args += fmt.Sprintf(",hostfwd=%s::%s-:%s", protocol, bind, port.Port)
		}
		for j, port := range route.TCP {
			bind, nr, err := virtualizers.BindPort(v.networkType, protocol, port.Port)
			if err != nil {
				v.log("error", "Error binding port: %v", err)
				return nil, err
			}
			v.routes[i].TCP[j].Address = nr
			hasDefinedPorts = true
			args += fmt.Sprintf(",hostfwd=%s::%s-:%s", protocol, bind, port.Port)
		}
		for j, port := range route.UDP {
			protocol = "udp"
			bind, nr, err := virtualizers.BindPort(v.networkType, protocol, port.Port)
			if err != nil {
				v.log("error", "Error binding port: %v", err)

				return nil, err
			}
			v.routes[i].UDP[j].Address = nr
			hasDefinedPorts = true
			args += fmt.Sprintf(",hostfwd=%s::%s-:%s", protocol, bind, port.Port)
		}
		nicArgs += fmt.Sprintf(" -netdev user,id=network%v%s -device virtio-net-pci,netdev=network%v,id=virtio%v,mac=26:10:05:00:00:0%x", i, args, i, i, 0xa+(i*0x1))
	}

	if noNic > 0 && !hasDefinedPorts {
		v.log("warning", "VM has network cards but no defined ports")
	}
	v.log("info", "Network Arguments: %s", nicArgs)

	return shellwords.Parse(nicArgs)
}

// State returns the state of the virtual machine
func (v *Virtualizer) State() string {
	return v.state
}

// Download returns the disk
func (v *Virtualizer) Download() (file.File, error) {
	v.log("debug", "Downloading Disk")

	if !(v.state == virtualizers.Ready) {
		return nil, fmt.Errorf("the machine must be in a stopped or ready state")
	}

	f, err := file.LazyOpen(v.disk.Name())
	if err != nil {
		return nil, err
	}

	return f, nil
}

// Detach removes the vm from the list and moves contents out of temp to source and writes shell script to run qemu
func (v *Virtualizer) Detach(source string) error {
	if v.state != virtualizers.Ready {
		return errors.New("virtual machine must be in a ready state to detach")
	}

	// generate bat or bash script to start qemu machine and copy disk to source folder
	err := v.GenerateScript(source)
	if err != nil {
		return err
	}

	// close file now as its not needed
	err = v.Close(false)
	if err != nil {
		return err
	}

	return nil
}

// Close shuts down the virtual machine and cleans up the disk and folders
func (v *Virtualizer) Close(force bool) error {
	v.log("debug", "Deleting VM")
	if force && !(v.state == virtualizers.Ready) {
		err := v.ForceStop()
		if err != nil {
			return err
		}
	}

	if !(v.state == virtualizers.Ready) {
		err := v.Stop()
		if err != nil {
			return err
		}
	}

	v.state = virtualizers.Deleted
	// v.subServer.SubServer.Publish(graph.VMUpdater)

	// remove virtualizer from active
	virtualizers.ActiveVMs.Delete(v.name)
	// kill process started from exec
	if runtime.GOOS != "windows" {
		if v.command.Process != nil {
			v.log("debug", "Killing Process")
			if err := v.command.Process.Kill(); err != nil && !strings.Contains(err.Error(), "process already finished") {
				return err
			}
		}
	} else {
		// windows sleep to allow me to clean up the files
		time.Sleep(time.Second * 4)
	}

	// remove contents when closing
	err := os.RemoveAll(v.folder)
	if err != nil {
		return err
	}
	return nil
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

// Prepare prepares the virtualizer with the appropriate fields to run the virtual machine
func (v *Virtualizer) Prepare(args *virtualizers.PrepareArgs) *virtualizers.VirtualizeOperation {

	op := new(operation)
	op.Virtualizer = v
	v.name = args.Name
	v.pname = args.PName
	v.created = time.Now()
	v.config = args.Config
	v.source = args.Source
	v.vmdrive = args.VMDrive
	v.virtLogger = logger.NewLogger(2048)
	v.serialLogger = logger.NewLogger(2048 * 10)
	v.log("debug", "Preparing VM")
	v.routes = v.Routes()
	// v.subServer = args.Subserver
	// v.subServer.SubServer.Publish(graph.VMUpdater)
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
