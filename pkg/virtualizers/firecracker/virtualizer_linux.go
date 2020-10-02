// +build linux

package firecracker

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	"github.com/milosgajdos/tenus"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
	"github.com/vorteil/vorteil/pkg/virtualizers/util"
)

const (
	vorteilBridge = "vorteil-bridge"
)

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

// Format writes the message differently compared to logsrus
func (f *firecrackerFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	return []byte(entry.Message), nil
}

func (o *operation) initializeVM(args *virtualizers.PrepareArgs) error {
	o.updateStatus(fmt.Sprintf("Building firecracker command and tap interfaces..."))

	o.state = "initializing"
	o.name = args.Name
	err := os.MkdirAll(args.FCPath, os.ModePerm)
	if err != nil {
		return err
	}
	o.folder = filepath.Dir(args.ImagePath)
	o.id = strings.Split(filepath.Base(o.folder), "-")[1]

	o.gctx = context.Background()
	o.vmmCtx, o.vmmCancel = context.WithCancel(o.gctx)

	o.kip, err = o.fetchVMLinux(fmt.Sprintf("firecracker-%s", o.config.VM.Kernel))
	if err != nil {
		return err
	}

	return nil
}

// prepare sets the fields and arguments to spawn the virtual machine
func (o *operation) prepare(args *virtualizers.PrepareArgs) {
	var returnErr error
	defer func() {
		o.finished(returnErr)
	}()

	diskpath := filepath.ToSlash(args.ImagePath)

	err := o.initializeVM(args)
	if err != nil {
		returnErr = err
		return
	}
	err = o.sendDeviceCreation()
	if err != nil {
		returnErr = err
		return
	}

	fcCfg, machineOpts := o.generateFirecrackerConfig(diskpath)
	// append new fields to overarching struct
	o.machineOpts = machineOpts
	o.fconfig = fcCfg

	o.state = "ready"

	_, loaded := virtualizers.ActiveVMs.LoadOrStore(o.name, o.Virtualizer)
	if loaded {
		returnErr = errors.New("virtual machine already exists")
		return
	}

	if args.Start {
		err = o.Start()
		if err != nil {
			returnErr = err
			return
		}
	}
}

func (o *operation) generateFirecrackerConfig(diskpath string) (firecracker.Config, []firecracker.Opt) {
	logger := log.New()
	logger.SetFormatter(&firecrackerFormatter{log.TextFormatter{
		DisableColors: false,
		ForceColors:   true,
	}})
	logger.Out = o
	devices := []models.Drive{}

	rootDrive := models.Drive{
		DriveID:      firecracker.String("1"),
		PathOnHost:   &diskpath,
		IsRootDevice: firecracker.Bool(true),
		IsReadOnly:   firecracker.Bool(false),
		Partuuid:     vimg.Part2UUIDString,
	}

	devices = append(devices, rootDrive)
	var interfaces []firecracker.NetworkInterface

	for i := 0; i < len(o.tapDevice.Devices); i++ {
		interfaces = append(interfaces,
			firecracker.NetworkInterface{
				StaticConfiguration: &firecracker.StaticNetworkConfiguration{
					HostDevName: o.tapDevice.Devices[i],
				},
			},
		)
	}
	return firecracker.Config{
			SocketPath:      filepath.Join(o.folder, fmt.Sprintf("%s.%s", o.name, "socket")),
			KernelImagePath: o.kip,
			KernelArgs:      fmt.Sprintf("loglevel=4 init=/vorteil/vinitd root=PARTUUID=%s reboot=k panic=1 pci=off vt.color=0x00", vimg.Part2UUIDString),
			Drives:          devices,
			MachineCfg: models.MachineConfiguration{
				VcpuCount:  firecracker.Int64(int64(o.config.VM.CPUs)),
				HtEnabled:  firecracker.Bool(false),
				MemSizeMib: firecracker.Int64(int64(o.config.VM.RAM.Units(vcfg.MiB))),
			},
			NetworkInterfaces: interfaces,
			ForwardSignals:    []os.Signal{},
		}, []firecracker.Opt{
			firecracker.WithLogger(log.NewEntry(logger)),
		}
}

func (o *operation) sendDeviceCreation() error {
	cd := CreateDevices{
		Id:     o.id,
		Routes: len(o.routes),
	}

	cdm, err := json.Marshal(cd)
	if err != nil {

		return err
	}
	resp, err := http.Post("http://localhost:7476/", "application/json", bytes.NewBuffer(cdm))
	if err != nil {
		return errors.New("Run 'sudo vorteil firecracker-setup' for the listener")
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var ifs Devices
	err = json.Unmarshal(body, &ifs)
	if err != nil {

		return err
	}

	o.tapDevice = ifs
	return nil
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
