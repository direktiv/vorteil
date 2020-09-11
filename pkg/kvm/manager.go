package kvm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"code.vorteil.io/toolkit/cli/pkg/compiler"
	"code.vorteil.io/toolkit/cli/pkg/vm"
	"code.vorteil.io/toolkit/cli/pkg/vm/hype"
	"code.vorteil.io/toolkit/cli/pkg/vm/shared"
	"code.vorteil.io/toolkit/cli/pkg/vm/util"
	shellwords "github.com/mattn/go-shellwords"
	randstr "github.com/thanhpk/randstr"
)

const hypervisor = hype.KVM

// Manager ...
type Manager struct {
	shared.VirtualMachineManager
}

// New ..
func New(args shared.LocalHypervisorManagerArgs) (*Manager, error) {

	if ok, err := util.IsInstalled(hype.Hypervisors[hype.KVM]); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("kvm executable is not installed on the PATH")
	}

	vmm, err := shared.NewLocalHypervisor(&args, hypervisor)
	if err != nil {
		return nil, err
	}
	vmm.PDiskFormat = compiler.ImageFormatVMDK.String()

	if args.NetworkType == shared.Bridged {
		// isBridged, err := isNetworkBridged(args.NetworkName)
		// if err != nil {
		// 	return nil, err
		// }
		// if !isBridged {
		// 	return nil, fmt.Errorf("Network interface '%s', is not a bridged network", args.NetworkName)
		// }
	}

	// Initilize variables
	manager := &Manager{
		VirtualMachineManager: *vmm,
	}

	return manager, nil
}

// EditManager ...
func (manager *Manager) EditManager(args *vm.ManagerArgsInterface) error {

	a := (*args).(*shared.LocalHypervisorManagerArgs)

	if args == nil {
		return nil
	}
	manager.Info("Editing %s...", hype.Hypervisors[hypervisor])

	if a.Verbose && manager.Verbose != a.Verbose {
		manager.Debug("Setting verbose to %v", a.Verbose)
		manager.Verbose = a.Verbose
	}
	if a.Headless && manager.Headless != a.Headless {
		manager.Debug("Setting headless to %v", a.Headless)
		manager.Headless = a.Headless
		manager.VmsMutex.RLock()
		for _, k := range manager.Vms {

			newArg := "sdl"
			if a.Headless {
				newArg = "none"
			}
			manager.Debug("Updating [%s] command to use %s %s. Will take affect on next Start", (*k).(*KVM).Name, "-display", newArg)
			for i, arg := range (*k).(*KVM).CommandArgs {
				if arg == "-display" {
					(*k).(*KVM).CommandArgs[i+1] = newArg
					break
				}
			}
		}
		manager.VmsMutex.RUnlock()
	}
	if a.TempDir != "" && manager.TempDir != a.TempDir {
		manager.Warn("Cannot update the temporary directory")
	}
	if a.DiskFormat != "" && manager.PDiskFormat != a.DiskFormat {
		manager.Warn("Cannot update the disk format")
	}
	if manager.NetworkType != a.NetworkType {
		manager.Debug("Setting network type to %v", a.NetworkType)
		manager.NetworkType = a.NetworkType
	}
	if a.NetworkName != "" && manager.NetworkName != a.NetworkName {
		manager.Debug("Setting network name to %v", a.NetworkName)
		manager.NetworkName = a.NetworkName
	}

	if a.NetworkType == shared.Bridged {
		if err := shared.ValidateNetworkExists(a.NetworkName); err != nil {
			return err
		}
		isBridged, err := isNetworkBridged(a.NetworkName)
		if err != nil {
			return err
		}
		if !isBridged {
			return fmt.Errorf("Network interface '%s', is not a bridged network", a.NetworkName)
		}
	}

	return nil
}

//LoadDetails is used to gather KVM details of the platform
func (manager *Manager) LoadDetails() (*vm.ManagerDetails, error) {

	d, err := manager.VirtualMachineManager.LoadDetails()
	if err != nil {
		manager.Debug("Error for load details kvm: %v", err)
	}

	b, err := json.MarshalIndent(map[string]interface{}{
		"platform-type": "kvm",
	}, "", "\t")
	if err != nil {
		manager.Debug("%v", err)
		return nil, nil
	}

	err = json.Unmarshal(b, &d.More)
	if err != nil {
		manager.Debug("%v", err)
		return nil, nil
	}

	return d, nil
}

// LoadState ...
func (manager *Manager) LoadState(data []byte) error {

	manager.VirtualMachineManager.LoadState(data)

	i := new(interface{})

	err := json.Unmarshal(data, i)
	if err != nil {
		manager.Error("Error unmarshaling data")
		manager.VerboseDebug("Data: [%+v]", data)
		return err
	}

	t := (*i).(map[string]interface{})

	field := t["Vms"]
	var vs map[string]interface{}
	if field != nil {
		vs = t["Vms"].(map[string]interface{})
	}

	vms := make(map[string]*vm.VM, len(vs))

	for key, value := range vs {
		var lv vm.VM
		v := value.(map[string]interface{})

		var ca []string

		if v["CommandArgs"] != nil {
			argsInterface := v["CommandArgs"].([]interface{})
			ca = make([]string, len(argsInterface))

			for i, arg := range argsInterface {
				ca[i] = arg.(string)
			}
		}

		nt := shared.NAT
		nn := ""
		if v["NetworkType"] != nil {
			nt = shared.NetworkType(v["NetworkType"].(float64))
		}
		if v["NetworkName"] != nil {
			nn = v["NetworkName"].(string)
		}

		lv = &KVM{
			VirtualMachine: *manager.UnmarshalVirtualMachineData(value),
			CommandArgs:    ca,
			NetworkType:    nt,
			NetworkName:    nn,
		}
		vms[key] = &lv

		lv.(*KVM).SetManager(&manager.VirtualMachineManager)
		lv.(*KVM).InitLogger(manager.GetLogger().GetHandler(), hypervisor)

		_, err := os.Stat(lv.(*KVM).TempDisk)
		if err != nil {
			lv.Delete(context.Background())
			delete(vms, key)
		}
	}

	manager.VirtualMachineManager.Vms = vms
	manager.VirtualMachineManager.PDiskFormat = t["PDiskFormat"].(string)
	manager.Headless = t["Headless"].(bool)
	manager.NetworkType = shared.NetworkType(t["NetworkType"].(float64))
	manager.NetworkName = t["NetworkName"].(string)
	manager.Verbose = t["Verbose"].(bool)

	manager.TempDir = t["TempDir"].(string)
	if manager.TempDir == "" {
		manager.Warn("No temp directory set in load data. It is recommended you recreate this %s platform", hype.Hypervisors[hypervisor])
		if err := manager.CreateTempDir(manager.TempDir, hypervisor); err != nil {
			manager.Error("Error creating temp dir: %v", err)
		}
	}

	// initilize the logging for each of the vms being loaded
	manager.VmsMutex.RLock()
	for _, v := range vms {
		var k *KVM
		k = (*v).(*KVM)

		// If the vm was previously alive or paused it's state is now ready.
		if k.State == vm.Alive || k.State == vm.Paused {
			k.State = vm.Ready
		}
	}
	manager.VmsMutex.RUnlock()

	manager.Debug("Manager state loaded")

	return nil
}

// Create ...
func (manager *Manager) Create(args *vm.CreateVMArgs) (vm.VM, error) {

	var v vm.VM
	v = &KVM{
		VirtualMachine: *(manager.VirtualMachineManager.Create(args, hypervisor).(*shared.VirtualMachine)),
		NetworkType:    manager.NetworkType,
		NetworkName:    manager.NetworkName,
	}
	manager.VmsMutex.Lock()
	manager.Vms[v.ID()] = &v
	manager.VmsMutex.Unlock()

	go func() {

		kvm := (v).(*KVM)
		defer manager.CreatePanicRecover(&kvm.VirtualMachine)
		var err error

		// Persist the temp dir
		tempParentDir := manager.TempDir

		if args.Persist && args.PersistPath != "" {
			tempParentDir = args.PersistPath
		}

		kvm.TempDir, err = kvm.CreateTempDir(tempParentDir)
		if err != nil {
			manager.Error("Error creating temp directory for vm '%s': %v", kvm.Name, err)
			kvm.UpdateState(vm.Broken)
			return
		}
		kvm.TempDisk, err = manager.CreateTempDisk(kvm.TempDir, args.VirtualDisk)
		if err != nil {
			manager.Error("Error creating temp disk for vm '%s': %v", kvm.Name, err)
			kvm.UpdateState(vm.Broken)
			return
		}

		kvm.Command, err = manager.createCommand(kvm, args)
		if err != nil {
			manager.Error("Error creating start command for vm '%s': %v", kvm.Name, err)
			kvm.UpdateState(vm.Broken)
			return
		}
		kvm.CommandArgs = kvm.Command.Args[1:]

		kvm.UpdateState(vm.Ready)
	}()

	return v, nil
}

// creates the command that can be executed to launch a kvm instance
func (manager *Manager) createCommand(kvm *KVM, args *vm.CreateVMArgs) (*exec.Cmd, error) {

	manager.VerboseDebug("Creating command to launch %v virtual machine", hypervisor)

	executable, err := hype.GetExecutable(hype.Hypervisors[hypervisor])
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}

	a, err := manager.paramsKVM(kvm, args)
	if err != nil {
		return nil, err
	}

	command := exec.Command(executable, a...)

	manager.Info("creating KVM with args: %s", strings.Join(command.Args, " "))
	manager.Debug("creating KVM with args: %s", strings.Join(command.Args, " "))
	manager.Debug("Network arguments still to be appended")

	return command, nil
}

// creates the parameters for the command that will be used when the kvm is executed
func (manager *Manager) paramsKVM(kvm *KVM, args *vm.CreateVMArgs) ([]string, error) {

	str := fmt.Sprintf("%s -no-reboot -machine q35 -smp %v -m %v -serial stdio", osFlags, args.CPUs, args.Memory)

	// headless
	if manager.Headless {
		str += fmt.Sprintf(" -display none")
	} else {
		str += fmt.Sprintf(" -display sdl")
	}

	// set the disk format
	// we might be able to refactor this once we know all supported disk formats for the different hypervisors.
	diskFormat := manager.PDiskFormat
	if args.DiskFormat == "raw" {
		diskFormat = args.DiskFormat
	}

	// virtio scsi hd
	diskpath := kvm.TempDisk
	diskpath = filepath.ToSlash(diskpath)
	str = str + fmt.Sprintf(" -device virtio-scsi-pci,id=scsi -device scsi-hd,drive=hd0 -drive if=none,file=\"%s\",format=%s,id=hd0", diskpath, diskFormat)
	// unix socket to send shutdown requests
	kvm.winPipe = randstr.Hex(5)
	if runtime.GOOS == "windows" {
		str = str + fmt.Sprintf(" -monitor pipe:%s", kvm.winPipe)
	} else {
		str = str + fmt.Sprintf(" -monitor unix:%s/monitor.sock,server,nowait", kvm.TempDir)
	}

	return shellwords.Parse(str)
}

func isNetworkBridged(nic string) (bool, error) {

	// potentially use https://github.com/milosgajdos83/tenus to create bridge networks
	command := exec.Command("ip", "-br", "link", "show", "type", "bridge")
	var errS bytes.Buffer
	command.Stdout = &errS

	if err := command.Run(); err != nil {
		return false, err
	}

	lines := strings.Split(fmt.Sprint(command.Stdout), "\n")

	for _, line := range lines {
		if len(line) <= 0 {
			continue
		}
		if nic == strings.Fields(line)[0] {
			return true, nil
		}
	}

	return false, nil
}
