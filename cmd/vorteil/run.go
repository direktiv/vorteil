package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"

	isatty "github.com/mattn/go-isatty"
	"github.com/mitchellh/go-homedir"
	"github.com/thanhpk/randstr"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	"github.com/vorteil/vorteil/pkg/virtualizers/firecracker"
	"github.com/vorteil/vorteil/pkg/virtualizers/hyperv"
	"github.com/vorteil/vorteil/pkg/virtualizers/qemu"
	"github.com/vorteil/vorteil/pkg/virtualizers/virtualbox"
	"github.com/vorteil/vorteil/pkg/vpkg"
)

func runFirecracker(pkgReader vpkg.Reader, cfg *vcfg.VCFG) error {
	if runtime.GOOS != "linux" {
		return errors.New("firecracker is only available on linux")
	}
	if !firecracker.Allocator.IsAvailable() {
		return errors.New("firecracker is not installed on your system")
	}
	// Check if bridge device exists
	err := firecracker.FetchBridgeDev()
	if err != nil {
		return errors.New("try running 'vorteil firecracker-setup' before using firecracker")
	}

	// Create base folder to store virtualbox vms so the socket can be grouped
	parent := fmt.Sprintf("%s-%s", firecracker.VirtualizerID, randstr.Hex(5))
	parent = filepath.Join(os.TempDir(), parent)

	// Create parent directory as it doesn't exist
	err = os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	f, err := ioutil.TempFile(parent, "vorteil.disk")
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	defer os.Remove(parent)

	err = vdisk.Build(context.Background(), f, &vdisk.BuildArgs{
		PackageReader: pkgReader,
		Format:        firecracker.Allocator.DiskFormat(),
		KernelOptions: vdisk.KernelOptions{
			Shell: flagShell,
		},
	})
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	err = f.Close()
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	err = pkgReader.Close()
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	alloc := firecracker.Allocator
	virt := alloc.Alloc()
	defer virt.Close(true)

	if flagGUI {
		log.Warn("firecracker does not support displaying a gui")
	}

	config := firecracker.Config{}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f.Name(), cfg)
}

func runHyperV(pkgReader vpkg.Reader, cfg *vcfg.VCFG) error {
	if runtime.GOOS != "windows" {
		return errors.New("hyper-v is only available on windows system")
	}
	if !hyperv.Allocator.IsAvailable() {
		return errors.New("hyper-v is not enabled on your system")
	}
	// Create base folder to store virtualbox vms so the socket can be grouped
	parent := fmt.Sprintf("%s-%s", hyperv.VirtualizerID, randstr.Hex(5))
	parent = filepath.Join(os.TempDir(), parent)

	// Create parent directory as it doesn't exist
	err := os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	// need to create a tempfile rather than use the function to as hyper-v complains if the extension doesn't exist
	f, err := os.Create(filepath.Join(parent, "disk.vhd"))
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	defer os.Remove(f.Name())
	defer f.Close()

	defer os.Remove(parent)

	err = vdisk.Build(context.Background(), f, &vdisk.BuildArgs{
		PackageReader: pkgReader,
		Format:        hyperv.Allocator.DiskFormat(),
		KernelOptions: vdisk.KernelOptions{
			Shell: flagShell,
		},
	})
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	err = f.Close()
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	err = pkgReader.Close()
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	alloc := hyperv.Allocator
	virt := alloc.Alloc()
	defer virt.Close(true)

	config := hyperv.Config{
		Headless:   !flagGUI,
		SwitchName: "Default Switch",
	}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f.Name(), cfg)
}

func runVirtualBox(pkgReader vpkg.Reader, cfg *vcfg.VCFG) error {
	if !virtualbox.Allocator.IsAvailable() {
		return errors.New("virtualbox not found installed on system")
	}
	// Create base folder to store virtualbox vms so the socket can be grouped
	parent := fmt.Sprintf("%s-%s", virtualbox.VirtualizerID, randstr.Hex(5))
	parent = filepath.Join(os.TempDir(), parent)

	// Create parent directory as it doesn't exist
	err := os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	f, err := ioutil.TempFile(parent, "vorteil.disk")
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer os.Remove(f.Name())
	defer f.Close()

	defer os.Remove(parent)

	err = vdisk.Build(context.Background(), f, &vdisk.BuildArgs{
		PackageReader: pkgReader,
		Format:        virtualbox.Allocator.DiskFormat(),
		KernelOptions: vdisk.KernelOptions{
			Shell: flagShell,
		},
	})
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	err = f.Close()
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	err = pkgReader.Close()
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	alloc := virtualbox.Allocator
	virt := alloc.Alloc()
	defer virt.Close(true)

	config := virtualbox.Config{
		Headless:    !flagGUI,
		NetworkType: "nat",
	}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f.Name(), cfg)
}

func runQEMU(pkgReader vpkg.Reader, cfg *vcfg.VCFG) error {

	if !qemu.Allocator.IsAvailable() {
		return errors.New("qemu not installed on system")
	}
	// Create base folder to store virtualbox vms so the socket can be grouped
	parent := fmt.Sprintf("%s-%s", qemu.VirtualizerID, randstr.Hex(5))
	parent = filepath.Join(os.TempDir(), parent)

	// Create parent directory as it doesn't exist
	err := os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	f, err := ioutil.TempFile(parent, "vorteil.disk")
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	defer os.Remove(parent)

	err = vdisk.Build(context.Background(), f, &vdisk.BuildArgs{
		PackageReader: pkgReader,
		Format:        qemu.Allocator.DiskFormat(),
		KernelOptions: vdisk.KernelOptions{
			Shell: flagShell,
		},
	})
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	err = f.Close()
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	err = pkgReader.Close()
	if err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}

	alloc := qemu.Allocator
	virt := alloc.Alloc()
	defer virt.Close(true)

	config := qemu.Config{
		Headless: !flagGUI,
	}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f.Name(), cfg)

}

func run(virt virtualizers.Virtualizer, diskpath string, cfg *vcfg.VCFG) error {

	// Gather home directory for firecracker storage path
	home, err := homedir.Dir()
	if err != nil {
		return err
	}

	_ = virt.Prepare(&virtualizers.PrepareArgs{
		Name:      "vorteil-vm",
		PName:     virt.Type(),
		Start:     true,
		Config:    cfg,
		FCPath:    filepath.Join(home, ".vorteild", "firecracker-vm"),
		ImagePath: diskpath,
	})

	serial := virt.Serial()
	virtualizerLogs := virt.Logs()
	defer virtualizerLogs.Close()
	defer serial.Close()
	virtSubscription := virtualizerLogs.Subscribe()
	serialSubscription := serial.Subscribe()
	defer virtSubscription.Close()
	defer serialSubscription.Close()
	s := serialSubscription.Inbox()
	v := virtSubscription.Inbox()

	signalChannel, chBool := listenForInterupt()

	var finished bool
	for {
		select {
		case <-time.After(time.Millisecond * 200):
			if finished && virt.State() == "ready" {
				virt.Close(false)
				return nil
			}
		case msg, more := <-v:
			if !more {
				virt.Close(false)
				return nil
			}
			fmt.Print(string(msg))
		case msg, more := <-s:
			if !more {
				virt.Close(false)
				return nil
			}
			fmt.Print(string(msg))
		case <-signalChannel:
			if finished {
				return nil
			}
			finished = true
			virt.Stop()
		case <-chBool:
			return nil
		}
	}

}

func raw(start bool) error {
	r := "raw"
	if !start {
		r = "-raw"
	}

	rawMode := exec.Command("stty", r)
	rawMode.Stdin = os.Stdin
	err := rawMode.Run()
	if err != nil {
		return err
	}

	return nil
}

func listenForInterupt() (chan os.Signal, chan bool) {
	var signalChannel chan os.Signal
	signalChannel = make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, os.Kill)
	chBool := make(chan bool, 1)

	// check if this is running in a sygwin terminal, interupt signals are difficult to capture
	if isatty.IsCygwinTerminal(os.Stdout.Fd()) {

		go func() {
			raw(true)
			for {
				inp := bufio.NewReader(os.Stdin)
				r, _, _ := inp.ReadRune()

				if r == '\x03' { // ctrl+c
					chBool <- true
					break
				}
			}
			raw(false)
		}()
	}
	return signalChannel, chBool
}
