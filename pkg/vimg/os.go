package vimg

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/vorteil/vorteil/pkg/vcfg"

	"github.com/mattn/go-shellwords"
	"github.com/vorteil/vorteil/pkg/vkern"
)

// Various build constants.
const (
	OSReservedSectors        = 32
	KernelConfigSpaceSectors = 32
)

// GetKernel is the function a Builder will use to load a kernel bundle. It must be set externally.
var GetKernel func(ctx context.Context, version vkern.CalVer) (*vkern.ManagedBundle, error)

// GetLatestKernel is the function a Builder will use to determine what kernel to use if none is specified. It must be set externally.
var GetLatestKernel func(ctx context.Context) (vkern.CalVer, error)

func (b *Builder) prebuildOS(ctx context.Context) error {

	err := b.calculateOSPartitionSize()
	if err != nil {
		return err
	}

	return nil
}

func (b *Builder) loadKernel(ctx context.Context) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	b.kernelBundle, err = GetKernel(ctx, b.kernel)
	if err != nil {
		return err
	}

	return nil

}

func (b *Builder) calculateMinimumOSPartitionSize(ctx context.Context) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	err = b.loadKernel(ctx)
	if err != nil {
		return err
	}

	err = b.generateConfig()
	if err != nil {
		return err
	}

	sectors := int64(KernelConfigSpaceSectors) // bootloader config

	// kernel size
	s := b.kernelBundle.Bundle().Size(b.kernelTags...)
	s = (s + SectorSize - 1) / SectorSize
	sectors += s

	// config size
	s = int64(len(b.configData))
	s = (s + SectorSize - 1) / SectorSize
	sectors += s

	// reserved space
	sectors += OSReservedSectors

	b.minSize += sectors * SectorSize

	return nil

}

func (b *Builder) calculateOSPartitionSize() error {

	b.osFirstLBA = P0FirstLBA

	sectors := int64(KernelConfigSpaceSectors) // bootloader config

	// kernel size
	s := b.kernelBundle.Bundle().Size(b.kernelTags...)
	s = (s + SectorSize - 1) / SectorSize
	sectors += s

	b.configFirstLBA = b.osFirstLBA + sectors

	// config size
	s = int64(len(b.configData))
	s = (s + SectorSize - 1) / SectorSize
	sectors += s

	// reserved space
	sectors += OSReservedSectors

	b.osLastLBA = b.osFirstLBA + sectors - 1

	return nil
}

func (b *Builder) setConfigDefaults() error {

	for i := range b.vcfg.Programs {

		p := &b.vcfg.Programs[i]

		if p.Cwd == "" {
			p.Cwd = "/"
		}

		if p.Stdout == "" {
			p.Stdout = "/dev/vtty"
		}

		if p.Stderr == "" {
			p.Stderr = "/dev/vtty"
		}

		if string(p.Privilege) == "" {
			p.Privilege = vcfg.RootPrivilege
		}

	}

	for i := range b.vcfg.Networks {

		n := &b.vcfg.Networks[i]

		if n.MTU == 0 {
			n.MTU = b.defaultMTU
		}

		if n.IP == "" {
			n.IP = "dhcp"
		}

	}

	if len(b.vcfg.System.DNS) == 0 {
		b.vcfg.System.DNS = []string{"8.8.8.8"}
	}

	if b.vcfg.System.MaxFDs == 0 {
		b.vcfg.System.MaxFDs = 1024
	}

	if b.vcfg.System.User == "" {
		b.vcfg.System.User = "root"
	}

	return nil
}

func (b *Builder) validateConfig() error {

	for i, p := range b.vcfg.Programs {

		if p.Binary == "" && p.Args == "" {
			return fmt.Errorf("missing binary and arguments for program %d", i)
		}

		switch p.Privilege {
		case vcfg.RootPrivilege:
		case vcfg.SuperuserPrivilege:
		case vcfg.UserPrivilege:
		default:
			return fmt.Errorf("invalid privilege setting for program %d: %s (should be 'root', 'superuser', or 'user')", i, p.Privilege)
		}

		// TODO: validate bootstrap?

	}

	for i, n := range b.vcfg.Networks {

		if n.IP == "dhcp" {
			if n.Mask != "" {
				return fmt.Errorf("network %d should not have a mask set when using dhcp", i)
			}

			if n.Gateway != "" {
				return fmt.Errorf("network %d should not have a mask set when using dhcp", i)
			}

			continue
		}

		if net.ParseIP(n.IP) == nil {
			return fmt.Errorf("network %d has an invalid static ip: %s", i, n.IP)
		}

		if net.ParseIP(n.Mask) == nil {
			return fmt.Errorf("network %d has an invalid mask: %s", i, n.Mask)
		}

		if net.ParseIP(n.Gateway) == nil {
			return fmt.Errorf("network %d has an invalid gateway: %s", i, n.Gateway)
		}

	}

	return nil

}

func (b *Builder) generateConfig() error {

	err := b.setConfigDefaults()
	if err != nil {
		return err
	}

	err = b.validateConfig()
	if err != nil {
		return err
	}

	data, err := json.Marshal(b.vcfg)
	if err != nil {
		return err
	}

	b.configData = data

	return nil
}

// BootloaderConfig is the structure of the bootloader config as it appears on the disk.
type BootloaderConfig struct {
	Version        [16]byte     // 0
	_              [16]byte     // 16
	LinuxArgsLen   uint16       // 32
	_              [6]byte      // 34
	ConfigOffset   uint64       // 40
	ConfigLen      uint64       // 48
	ConfigCapacity uint64       // 56
	_              [192]byte    // 64
	LinuxArgs      [0x2000]byte // 256
}

func (b *Builder) writeBootloader(ctx context.Context, w io.WriteSeeker) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	_, err = w.Seek(b.osFirstLBA*SectorSize, io.SeekStart)
	if err != nil {
		return err
	}

	bootConf := BootloaderConfig{
		LinuxArgsLen:   uint16(len(b.linuxArgs)),
		ConfigOffset:   uint64(b.configFirstLBA-b.osFirstLBA) * SectorSize,
		ConfigLen:      uint64(len(b.configData)),
		ConfigCapacity: uint64(b.osLastLBA-b.configFirstLBA+1) * SectorSize,
	}

	copy(bootConf.Version[:], "1.0.0")
	copy(bootConf.LinuxArgs[:], b.linuxArgs)

	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, &bootConf)

	_, err = io.Copy(w, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return err
	}

	return nil

}

func (b *Builder) writeKernel(ctx context.Context, w io.WriteSeeker) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	_, err = w.Seek((b.osFirstLBA+KernelConfigSpaceSectors)*SectorSize, io.SeekStart)
	if err != nil {
		return err
	}

	kern := b.kernelBundle.Bundle().Reader(b.kernelTags...)
	_, err = io.Copy(w, kern)
	if err != nil {
		return err
	}

	return nil

}

func (b *Builder) writeKernelConfig(ctx context.Context, w io.WriteSeeker) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	_, err = w.Seek(b.configFirstLBA*SectorSize, io.SeekStart)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, bytes.NewReader(b.configData))
	if err != nil {
		return err
	}

	return nil

}

func (b *Builder) writeKernelAndConfig(ctx context.Context, w io.WriteSeeker) error {

	err := b.writeKernel(ctx, w)
	if err != nil {
		return err
	}

	err = b.writeKernelConfig(ctx, w)
	if err != nil {
		return err
	}

	return nil

}

func (b *Builder) writeOS(ctx context.Context, w io.WriteSeeker) error {

	err := b.writeBootloader(ctx, w)
	if err != nil {
		return err
	}

	err = b.writeKernelAndConfig(ctx, w)
	if err != nil {
		return err
	}

	// padding
	_, err = w.Seek((b.osLastLBA+1)*SectorSize, io.SeekStart)
	if err != nil {
		return err
	}

	return nil
}

func (b *Builder) osRegionIsHole(begin, size int64) bool {
	first := begin / SectorSize
	if first >= b.osLastLBA-OSReservedSectors+1 {
		return true
	}
	return false
}

func (b *Builder) determineKernelTags() {

	b.kernelTags = []string{}

	if b.kernelOptions.Shell {
		b.kernelTags = append(b.kernelTags, "shell")
	}

	if len(b.vcfg.System.NTP) > 0 {
		b.kernelTags = append(b.kernelTags, "ntp")
	}

	if len(b.vcfg.Logging) > 0 {
		b.kernelTags = append(b.kernelTags, "logs")
	} else {
		for _, prog := range b.vcfg.Programs {
			if len(prog.LogFiles) > 0 {
				b.kernelTags = append(b.kernelTags, "logs")
				break
			}
		}
	}

	for _, prog := range b.vcfg.Programs {
		if prog.Strace {
			b.kernelTags = append(b.kernelTags, "strace")
			break
		}
	}

	for _, nic := range b.vcfg.Networks {
		if nic.TCPDUMP {
			b.kernelTags = append(b.kernelTags, "tcpdump")
			break
		}
	}

	return

}

func (b *Builder) validateOSArgs(ctx context.Context) error {

	b.linuxArgs = b.vcfg.System.KernelArgs

	b.determineKernelTags()

	var err error
	b.kernel, err = vkern.Parse(b.vcfg.VM.Kernel)
	if err != nil {
		if err == vkern.ErrInvalidCalVer {
			b.kernel, err = GetLatestKernel(ctx)
		}
		if err != nil {
			return err
		}
	} else if b.kernel.Less(vkern.CalVer("20.9.1")) {
		b.kernel, err = GetLatestKernel(ctx)
		if err != nil {
			return err
		}
		b.log.Warnf("Requested kernel '%s' is too old for this compiler. Using latest kernel instead.", b.vcfg.VM.Kernel)
	}

	err = b.processLinuxArgs()
	if err != nil {
		return err
	}

	return nil
}

func (b *Builder) processLinuxArgs() error {

	parser := shellwords.NewParser()
	parser.ParseEnv = false
	parser.ParseBacktick = false
	args, err := parser.Parse(b.linuxArgs)
	if err != nil {
		return err
	}

	m := make(map[string]int)
	for i, s := range args {
		m[strings.SplitN(s, "=", 2)[0]] = i
	}

	_, ok1 := m["ro"]
	_, ok2 := m["rw"]
	if !ok1 && !ok2 {
		args = append(args, "ro")
	}

	// if the fs is not set here we assume it is ext2
	fs := b.vcfg.System.Filesystem
	if fs == "" {
		fs = "ext2"
	}

	args = append(args, fmt.Sprintf("rootfstype=%s", fs))

	if _, ok := m["loglevel"]; !ok {
		args = append(args, "loglevel=4")
	}

	if _, ok := m["intel_idle.max_cstate"]; !ok {
		args = append(args, "intel_idle.max_cstate=0")
	}

	if _, ok := m["processor.max_cstate"]; !ok {
		args = append(args, "processor.max_cstate=1")
	}

	// console
	if _, ok := m["console"]; !ok {
		args = append(args, "console=ttyS0,115200", "console=tty0")
	} else {
		b.log.Warnf("system.kernel-args contains a 'console' argument, which interferes with the system.output-mode VCFG value")
	}

	if _, ok := m["init"]; !ok {
		args = append(args, "init=/vorteil/vinitd")
	}

	if _, ok := m["root"]; !ok {
		args = append(args, fmt.Sprintf("root=PARTUUID=%s", Part2UUIDString))
	}

	args = append(args, "i8042.noaux i8042.nomux i8042.nopnp i8042.dumbkbd vt.color=0x00")

	var x []string
	for _, s := range args {
		if strings.ContainsAny(s, " \t") {
			x = append(x, strconv.Quote(s))
		} else {
			x = append(x, s)
		}
	}
	b.linuxArgs = strings.Join(x, " ")

	b.log.Debugf("kernel args: %v", b.linuxArgs)

	return nil
}
