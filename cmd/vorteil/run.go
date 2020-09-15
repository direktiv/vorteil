package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"time"

	isatty "github.com/mattn/go-isatty"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	"github.com/vorteil/vorteil/pkg/virtualizers/firecracker"
	"github.com/vorteil/vorteil/pkg/virtualizers/hyperv"
	"github.com/vorteil/vorteil/pkg/virtualizers/qemu"
	"github.com/vorteil/vorteil/pkg/virtualizers/virtualbox"
)

func runFirecracker(diskpath string, cfg *vcfg.VCFG, gui bool) error {
	if runtime.GOOS != "linux" {
		return errors.New("firecracker is only available on linux")
	}

	if !firecracker.Allocator.IsAvailable() {
		return errors.New("firecracker is not installed on your system")
	}

	f, err := vio.Open(diskpath)
	if err != nil {
		return err
	}

	defer f.Close()

	alloc := firecracker.Allocator
	virt := alloc.Alloc()
	defer virt.Close(true)

	if gui {
		log.Warn("firecracker does not support displaying a gui")
	}

	config := firecracker.Config{}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f, cfg)
}

func runHyperV(diskpath string, cfg *vcfg.VCFG, gui bool) error {
	if runtime.GOOS != "windows" {
		return errors.New("hyper-v is only available on windows system")
	}
	if !hyperv.Allocator.IsAvailable() {
		return errors.New("hyper-v is not enabled on your system")
	}

	f, err := vio.Open(diskpath)
	if err != nil {
		return err
	}

	defer f.Close()

	alloc := hyperv.Allocator
	virt := alloc.Alloc()
	defer virt.Close(true)

	config := hyperv.Config{
		Headless: !gui,
	}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f, cfg)
}

func runVirtualBox(diskpath string, cfg *vcfg.VCFG, gui bool) error {

	if !virtualbox.Allocator.IsAvailable() {
		return errors.New("virtualbox not found installed on system")
	}

	f, err := vio.Open(diskpath)
	if err != nil {
		return err
	}

	defer f.Close()

	alloc := virtualbox.Allocator
	virt := alloc.Alloc()
	defer virt.Close(true)

	config := virtualbox.Config{
		Headless:    !gui,
		NetworkType: "nat",
	}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f, cfg)
}

func runQEMU(diskpath string, cfg *vcfg.VCFG, gui bool) error {

	if !qemu.Allocator.IsAvailable() {
		return errors.New("qemu not installed on system")
	}

	f, err := vio.Open(diskpath)
	if err != nil {
		return err
	}
	defer f.Close()

	alloc := qemu.Allocator
	virt := alloc.Alloc()
	defer virt.Close(true)

	config := qemu.Config{
		Headless: !gui,
	}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f, cfg)

}

func run(virt virtualizers.Virtualizer, disk vio.File, cfg *vcfg.VCFG) error {

	_ = virt.Prepare(&virtualizers.PrepareArgs{
		Name:   "vorteil-vm",
		PName:  "qemu",
		Start:  true,
		Config: cfg,
		Image:  disk,
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
				virt.Close(true)
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
			go virt.Stop()
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
