package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"github.com/thanhpk/randstr"
	"github.com/vorteil/vorteil/pkg/ext"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/virtualizers/firecracker"
	"github.com/vorteil/vorteil/pkg/virtualizers/hyperv"
	"github.com/vorteil/vorteil/pkg/virtualizers/iputil"
	"github.com/vorteil/vorteil/pkg/virtualizers/qemu"
	"github.com/vorteil/vorteil/pkg/virtualizers/virtualbox"
	"github.com/vorteil/vorteil/pkg/vpkg"
)

var ips *iputil.IPStack

// buildFirecracker does the same thing as vdisk.Build but it returns me a calver of the kernel being used
func buildFirecracker(ctx context.Context, w io.WriteSeeker, cfg *vcfg.VCFG, args *vdisk.BuildArgs) (string, error) {
	for i := range cfg.Networks {
		ip := ips.Pop()
		if ip == "" {
			return "", errors.New("no more ips in stack")

		}
		cfg.Networks[i].IP = ip
		cfg.Networks[i].Gateway = "10.26.10.1"
		cfg.Networks[i].Mask = "255.255.255.0"
	}
	vimgBuilder, err := vdisk.CreateBuilder(ctx, &vimg.BuilderArgs{
		Kernel: vimg.KernelOptions{
			Shell: args.KernelOptions.Shell,
		},
		FSCompiler: ext.NewCompiler(&ext.CompilerArgs{
			FileTree: args.PackageReader.FS(),
			Logger:   args.Logger,
		}),
		VCFG:   cfg,
		Logger: log,
	})
	if err != nil {
		return "", err
	}
	defer vimgBuilder.Close()
	vimgBuilder.SetDefaultMTU(args.Format.DefaultMTU())
	err = vdisk.NegotiateSize(ctx, vimgBuilder, cfg, args)
	if err != nil {
		return "", err
	}

	err = args.Format.Build(ctx, log, w, vimgBuilder, cfg)
	if err != nil {
		return "", err
	}
	return string(vimgBuilder.KernelUsed()), nil
}

// runFirecracker needs a longer build process so we can pull the calver of the kernel used to build the disk
func runFirecracker(pkgReader vpkg.Reader, cfg *vcfg.VCFG, name string) error {
	if runtime.GOOS != "linux" {
		return errors.New("firecracker is only available on linux")
	}
	if !firecracker.Allocator.IsAvailable() {
		return errors.New("firecracker is not installed on your system")
	}

	ips = iputil.NewIPStack()
	ip := ips.Pop()
	if ip == "" {
		return errors.New("no more ips in stack")
	}

	err := firecracker.SetupBridge(log, ip)
	if err != nil {
		return err
	}
	// Create base folder to store firecracker vms so the socket can be grouped
	parent := fmt.Sprintf("%s-%s", firecracker.VirtualizerID, randstr.Hex(5))
	parent = filepath.Join(os.TempDir(), parent)
	defer os.RemoveAll(parent)

	// Create parent directory as it doesn't exist
	err = os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		return err
	}

	f, err := ioutil.TempFile(parent, "vorteil.disk")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()

	defer os.Remove(parent)

	kernelVer, err := buildFirecracker(context.Background(), f, cfg, &vdisk.BuildArgs{
		PackageReader: pkgReader,
		Format:        firecracker.Allocator.DiskFormat(),
		KernelOptions: vdisk.KernelOptions{
			Shell: flagShell,
		},
		Logger: log,
	})
	if err != nil {
		return err
	}

	// assign kernel version that was built with vcfg
	cfg.VM.Kernel = kernelVer

	err = f.Close()
	if err != nil {
		return err
	}

	err = pkgReader.Close()
	if err != nil {
		return err
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
	// Create base folder to store hyper-v vms so the socket can be grouped
	parent := fmt.Sprintf("%s-%s", hyperv.VirtualizerID, randstr.Hex(5))
	parent = filepath.Join(os.TempDir(), parent)

	// Create parent directory as it doesn't exist
	err := os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		return err
	}

	// need to create a tempfile rather than use the function to as hyper-v complains if the extension doesn't exist
	f, err := os.Create(filepath.Join(parent, "disk.vhd"))
	if err != nil {
		return err
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
		return err
	}

	err = f.Close()
	if err != nil {
		return err
	}

	err = pkgReader.Close()
	if err != nil {
		return err
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
		return err
	}

	f, err := ioutil.TempFile(parent, "vorteil.disk")
	if err != nil {
		return err
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
		return err
	}

	err = f.Close()
	if err != nil {
		return err
	}

	err = pkgReader.Close()
	if err != nil {
		return err
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
	// Create base folder to store qemu vms so the socket can be grouped
	parent := fmt.Sprintf("%s-%s", qemu.VirtualizerID, randstr.Hex(5))
	parent = filepath.Join(os.TempDir(), parent)
	defer os.RemoveAll(parent)
	// Create parent directory as it doesn't exist
	err := os.MkdirAll(parent, os.ModePerm)
	if err != nil {
		return err
	}
	f, err := ioutil.TempFile(parent, "vorteil.disk")
	if err != nil {
		return err
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
		return err
	}

	err = f.Close()
	if err != nil {
		return err
	}

	err = pkgReader.Close()
	if err != nil {
		return err
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
