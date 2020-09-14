package firecracker

import (
	"encoding/json"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/virtualizers"
)

// VirtualizerID is a unique identifier for Firecracker
var VirtualizerID = "firecracker"

type allocator struct{}

// Config to run the virtualizer
type Config struct{}

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

// Allocator for Firecracker
var Allocator virtualizers.VirtualizerAllocator = &allocator{}

// Alloc returns a new virtualizer
func (a *allocator) Alloc() virtualizers.Virtualizer {
	return new(Virtualizer)
}

// DiskAlignment returns the alignment Firecracker requires to run properly
func (a *allocator) DiskAlignment() vcfg.Bytes {
	return 2 * vcfg.MiB
}

// DiskFormat return the format the hypervisor should be using
func (a *allocator) DiskFormat() vdisk.Format {
	return vdisk.RAWFormat
}

// IsAvailable returns true if the hypervisor is installed
func (a *allocator) IsAvailable() bool {
	installed, _ := virtualizers.Backends()
	for _, platform := range installed {
		if platform == "firecracker" {
			return true
		}
	}
	return false
}

// ValidateArgs check if valid args are passed to create a valid Virtualizer
func (a *allocator) ValidateArgs(data []byte) error {
	// nothing to validate return nil
	return nil
}

// Create creates a virtualizer using the provided manager
func Create(mgr *virtualizers.Manager, name string) error {
	c := new(Config)
	err := mgr.CreateVirtualizer(name, VirtualizerID, c.Marshal())
	if err != nil {
		return err
	}
	return nil
}
