package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/thanhpk/randstr"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/virtualizers/firecracker"
	"github.com/vorteil/vorteil/pkg/virtualizers/hyperv"
	"github.com/vorteil/vorteil/pkg/virtualizers/qemu"
	"github.com/vorteil/vorteil/pkg/virtualizers/virtualbox"
	"github.com/vorteil/vorteil/pkg/vpkg"
)

func runFirecracker(pkgReader vpkg.Reader, cfg *vcfg.VCFG, name string) error {
	if runtime.GOOS != "linux" {
		return errors.New("firecracker is only available on linux")
	}
	if !firecracker.Allocator.IsAvailable() {
		return errors.New("firecracker is not installed on your system")
	}
	// Check if bridge device exists
	err := firecracker.FetchBridgeDev()
	if err != nil {
		return errors.New("try running 'sudo vorteil firecracker-setup' before using firecracker")
	}

	// Create base folder to store virtualbox vms so the socket can be grouped
	parent := fmt.Sprintf("%s-%s", firecracker.VirtualizerID, randstr.Hex(5))
	parent = filepath.Join(os.TempDir(), parent)
	defer os.RemoveAll(parent)

	// Create parent directory as it doesn't exist
	err = os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	f, err := ioutil.TempFile(parent, "vorteil.disk")
	if err != nil {
		log.Errorf("%v", err)
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
		Logger: log,
	})
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	err = f.Close()
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	err = pkgReader.Close()
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	alloc := firecracker.Allocator
	virt := alloc.Alloc()

	if flagGUI {
		log.Warnf("firecracker does not support displaying a gui")
	}

	config := firecracker.Config{}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f.Name(), cfg, name)
}

func runHyperV(pkgReader vpkg.Reader, cfg *vcfg.VCFG, name string) error {
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
		log.Errorf("%v", err)
		os.Exit(1)
	}

	// need to create a tempfile rather than use the function to as hyper-v complains if the extension doesn't exist
	f, err := os.Create(filepath.Join(parent, "disk.vhd"))
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	defer os.Remove(f.Name())
	defer f.Close()

	defer os.RemoveAll(parent)

	err = vdisk.Build(context.Background(), f, &vdisk.BuildArgs{
		PackageReader: pkgReader,
		Format:        hyperv.Allocator.DiskFormat(),
		KernelOptions: vdisk.KernelOptions{
			Shell: flagShell,
		},
		Logger: log,
	})
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	err = f.Close()
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	err = pkgReader.Close()
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	alloc := hyperv.Allocator
	virt := alloc.Alloc()

	config := hyperv.Config{
		Headless:   !flagGUI,
		SwitchName: "Default Switch",
	}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f.Name(), cfg, name)
}

func runVirtualBox(pkgReader vpkg.Reader, cfg *vcfg.VCFG, name string) error {
	if !virtualbox.Allocator.IsAvailable() {
		return errors.New("virtualbox not found installed on system")
	}
	// Create base folder to store virtualbox vms so the socket can be grouped
	parent := fmt.Sprintf("%s-%s", virtualbox.VirtualizerID, randstr.Hex(5))
	parent = filepath.Join(os.TempDir(), parent)
	defer os.RemoveAll(parent)

	// Create parent directory as it doesn't exist
	err := os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	f, err := ioutil.TempFile(parent, "vorteil.disk")
	if err != nil {
		log.Errorf("%v", err)
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
		Logger: log,
	})
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	err = f.Close()
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	err = pkgReader.Close()
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	alloc := virtualbox.Allocator
	virt := alloc.Alloc()

	config := virtualbox.Config{
		Headless:    !flagGUI,
		NetworkType: "nat",
	}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f.Name(), cfg, name)
}

func runQEMU(pkgReader vpkg.Reader, cfg *vcfg.VCFG, name string) error {

	if !qemu.Allocator.IsAvailable() {
		return errors.New("qemu not installed on system")
	}
	// Create base folder to store virtualbox vms so the socket can be grouped
	parent := fmt.Sprintf("%s-%s", qemu.VirtualizerID, randstr.Hex(5))
	parent = filepath.Join(os.TempDir(), parent)
	defer os.RemoveAll(parent)
	// Create parent directory as it doesn't exist
	err := os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
	f, err := ioutil.TempFile(parent, "vorteil.disk")
	if err != nil {
		log.Errorf("%v", err)
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
		Logger: log,
	})
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	err = f.Close()
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	err = pkgReader.Close()
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	alloc := qemu.Allocator
	virt := alloc.Alloc()

	config := qemu.Config{
		Headless: !flagGUI,
	}

	err = virt.Initialize(config.Marshal())
	if err != nil {
		return err
	}

	return run(virt, f.Name(), cfg, name)

}
