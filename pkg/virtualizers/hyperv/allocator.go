package hyperv

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"encoding/json"
	"errors"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/virtualizers"
)

// VirtualizerID is a unique identifier for Hyperv
var VirtualizerID = "hyperv"

// Config required for creating a Hyper-V VM
type Config struct {
	Headless   bool
	SwitchName string
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

// Allocator for Hyper-V
var Allocator virtualizers.VirtualizerAllocator = &allocator{}

// Alloc returns a new Virtualizer
func (a *allocator) Alloc() virtualizers.Virtualizer {
	return new(Virtualizer)
}

// DiskAlignment returns the alignment Hyper-V requires to run properly
func (a *allocator) DiskAlignment() vcfg.Bytes {
	return 2 * vcfg.MiB
}

// IsAvailable returns true if the hypervisor is installed
func (a *allocator) IsAvailable() bool {
	installed, _ := virtualizers.Backends()

	for _, platform := range installed {
		if platform == "hyperv" {
			return true
		}
	}

	return false
}

// DiskFormat returns the format the hypervisor should be using
func (a *allocator) DiskFormat() vdisk.Format {
	return vdisk.VHDDynamicFormat
}

// ValidateArgs check if valid args are passed to create a valid Virtualizer
func (a *allocator) ValidateArgs(data []byte) error {
	c := new(Config)
	err := c.Unmarshal(data)
	if err != nil {
		return err
	}

	switches, err := virtualizers.VSwitches()
	if err != nil {
		return err
	}

	switchFound := false
	for _, name := range switches {
		if name == c.SwitchName {
			switchFound = true
		}
	}

	if !switchFound {
		return errors.New("switch not found in list")
	}

	return nil
}

// Create creates a virtualizer using the provided manager
func Create(mgr *virtualizers.Manager, name string, headless bool, switchName string) error {
	c := new(Config)
	c.Headless = headless
	c.SwitchName = switchName
	err := mgr.CreateVirtualizer(name, VirtualizerID, c.Marshal())
	if err != nil {
		return err
	}
	return nil
}
