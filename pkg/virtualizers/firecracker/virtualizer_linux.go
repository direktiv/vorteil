// +build linux

package firecracker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/milosgajdos/tenus"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
	"github.com/vorteil/vorteil/pkg/virtualizers/util"
)

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
	logger       elog.View      // logger
	serialLogger *logger.Logger // logs for the serial of the vm

	routes []virtualizers.NetworkInterface // api network interface that displays ports
	config *vcfg.VCFG                      // config for the vm

	firecrackerPath string // path to vmlinux files
	// firecracker related objects
	vmmCtx    context.Context    // virtualmachineManager firecracker context
	gctx      context.Context    // global context used to spawn vmm
	vmmCancel context.CancelFunc // cancel vmm

	fconfig     *firecracker.Config  // config for virtual machine manager
	machine     *firecracker.Machine // machine firecracker spawned
	machineOpts []firecracker.Opt    // options provided to spawn machine

	bridgeDevice *tenus.Bridger // bridge device e.g vorteil-bridge
	tapDevice    *Devices       // tap device for the machine

	vmdrive string // store disks in this directory
}

// Type returns the type of virtualizer
func (v *Virtualizer) Type() string {
	return VirtualizerID
}

// Initialize passes the arguments from creation to create a virtualizer. No arguments apart from name so no need to do anything
func (v *Virtualizer) Initialize(data []byte) error {
	return nil
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

		// API way of shutting down vm seems bugged going to send request myself for now.
		err := v.machine.Shutdown(v.vmmCtx)
		if err != nil {
			return err
		}

		// Sleep to handle shutdown logs doesn't affect anything makes the output nicer
		time.Sleep(time.Second * 2)

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

// Close shuts down the virtual machine and cleans up the disk and folders
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

	err = DeleteTapDevices(v.tapDevice.Devices)
	if err != nil {
		return err
	}

	v.state = virtualizers.Deleted

	// remove virtualizer from active vms
	virtualizers.ActiveVMs.Delete(v.name)

	return nil
}

// Details returns data to for the ConverToVM function on util
func (v *Virtualizer) Details() (string, string, string, []virtualizers.NetworkInterface, time.Time, *vcfg.VCFG, interface{}) {
	return v.name, v.pname, v.state, v.routes, v.created, v.config, v.source
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
	v.routes = util.Routes(v.config)

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

			v.machine, err = firecracker.NewMachine(v.vmmCtx, *v.fconfig, v.machineOpts...)
			if err != nil {
				v.logger.Errorf("Error creating machine: %s", err.Error())
			}

			if err := v.machine.Start(v.vmmCtx); err != nil {
				v.logger.Errorf("Error starting virtual machine: %s", err.Error())
			}
			v.state = virtualizers.Alive

			go func() {
				ips := util.LookForIP(v.serialLogger)
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
			}()

			if err := v.machine.Wait(v.vmmCtx); err != nil {
				// Should end when we ctrl-c no need to print this.
				if !strings.Contains(err.Error(), "* signal: interrupt") {
					v.logger.Errorf("Wait returned an error: %s", err.Error())
				}
			}
		}()
	}
	return nil
}
