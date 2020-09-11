package vmware

import (
	"encoding/json"
	"fmt"

	"code.vorteil.io/vorteil/tools/cli/pkg/compiler"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/virtualizers"
)

// VirtualizerID is a unique identifier for VMware
var VirtualizerID = "vmware"

// Config required for creating a VMware VM
type Config struct {
	Headless    bool
	NetworkType string
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

// Allocator for VMware
var Allocator virtualizers.VirtualizerAllocator = &allocator{}

// Alloc returns a new Virtualizer
func (a *allocator) Alloc() virtualizers.Virtualizer {
	return new(Virtualizer)
}

// DiskAlignment returns the alignment VMware requires to run properly.
func (a *allocator) DiskAlignment() size.Bytes {
	return 2 * size.MiB
}

// IsAvailable returns true if the hypervisor is installed.
func (a *allocator) IsAvailable() bool {
	installed, _ := virtualizers.Backends()
	for _, platform := range installed {
		if platform == "vmware" {
			return true
		}
	}
	return false
}

// DiskFormat return the format the hypervisor should be using
func (a *allocator) DiskFormat() vdisk.Format {
	return compiler.ImageFormatVMDK
}

// ValidateArgs check if valid args are passed to create a valid Virtualizer
func (a *allocator) ValidateArgs(data []byte) error {
	c := new(Config)
	err := c.Unmarshal(data)
	if err != nil {
		return err
	}

	// nat, bridged hostonly
	if c.NetworkType == "nat" || c.NetworkType == "bridged" || c.NetworkType == "hostonly" {
	} else {
		return fmt.Errorf("network type is not valid got %s but expected nat, bridged or hostonly", c.NetworkType)
	}

	return nil
}

// Create creates a virtualizer using the provided manager
func Create(mgr *virtualizers.Manager, name string, headless bool, networkType string) error {
	c := new(Config)
	c.Headless = headless
	c.NetworkType = networkType
	err := mgr.CreateVirtualizer(name, VirtualizerID, c.Marshal())
	if err != nil {
		return err
	}
	return nil
}
