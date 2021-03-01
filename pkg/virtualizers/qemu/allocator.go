package qemu

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"encoding/json"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"

	"github.com/vorteil/vorteil/pkg/virtualizers"
)

// VirtualizerID is a unique identifier for QEMU
var VirtualizerID = "qemu"

// Config required for creating a QEMU VM
type Config struct {
	Headless bool
}

// Marshal the config into a byte[]
func (c *Config) Marshal() []byte {
	data, err := json.Marshal(c)
	if err != nil {
		panic(err)
	}
	return data
}

// Unmarshal the byte[] array into a config struct
func (c *Config) Unmarshal(data []byte) error {
	err := json.Unmarshal(data, c)
	if err != nil {
		return err
	}
	return nil
}

type allocator struct {
}

// Allocator for QEMU
var Allocator virtualizers.VirtualizerAllocator = &allocator{}

// Alloc returns a new virtualizer
func (a *allocator) Alloc() virtualizers.Virtualizer {
	return new(Virtualizer)
}

// DiskAlignment returns the alignment QEMU requires to run properly.
func (a *allocator) DiskAlignment() vcfg.Bytes {
	return 2 * vcfg.MiB
}

// IsAvailable returns true if the hypervisor is installed.
func (a *allocator) IsAvailable() bool {
	installed, _ := virtualizers.Backends()

	for _, platform := range installed {
		if platform == "qemu" {
			return true
		}
	}

	return false
}

// DiskFormat return the format the hypervisor should be using.
func (a *allocator) DiskFormat() vdisk.Format {
	return vdisk.QCOW2Format // vdisk.RAWFormat
}

// ValidateArgs check if valid args are passed to create a valid Virtualizer
func (a *allocator) ValidateArgs(data []byte) error {
	c := new(Config)
	err := c.Unmarshal(data)
	if err != nil {
		return err
	}
	return nil
}

// Create creates a virtualizer using the provided manager
func Create(mgr *virtualizers.Manager, name string, headless bool) error {
	c := new(Config)
	c.Headless = headless
	err := mgr.CreateVirtualizer(name, VirtualizerID, c.Marshal())
	if err != nil {
		return err
	}
	return nil
}
