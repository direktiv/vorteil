package virtualbox

import (
	"encoding/json"
	"fmt"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/virtualizers"
)

// VirtualizerID is a unique identifer for VirtualBox
var VirtualizerID = "virtualbox"

// Config required for creating a VirtualBox VM
type Config struct {
	Headless      bool
	NetworkType   string
	NetworkDevice string
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

// Allocator for Virtualbox
var Allocator virtualizers.VirtualizerAllocator = &allocator{}

// Alloc returns a new Virtualizer
func (a *allocator) Alloc() virtualizers.Virtualizer {
	return new(Virtualizer)
}

// DiskAlignment returns the alignment Virtualbox requries to run properly
func (a *allocator) DiskAlignment() vcfg.Bytes {
	return 2 * vcfg.MiB
}

// IsAvailable returns true if the hypervisor is installed.
func (a *allocator) IsAvailable() bool {
	installed, _ := virtualizers.Backends()
	for _, platform := range installed {
		if platform == "virtualbox" {
			return true
		}
	}
	return false
}

// DiskFormat returns the format the hypervisor should be using.
func (a *allocator) DiskFormat() vdisk.Format {
	return vdisk.VMDKSparseFormat
}

// ValidateArgs check if valid args are passed to create a valid Virtualizer
func (a *allocator) ValidateArgs(data []byte) error {
	c := new(Config)
	err := c.Unmarshal(data)
	if err != nil {
		return err
	}

	if c.NetworkType == "bridged" {
		devices, err := virtualizers.BridgedDevices()
		if err != nil {
			return err
		}
		bridgedDevice := false
		for _, device := range devices {
			if device == c.NetworkDevice {
				bridgedDevice = true
			}
		}
		if !bridgedDevice {
			return fmt.Errorf("bridged device %s does not exist on virtualbox", c.NetworkDevice)
		}
	}

	if c.NetworkType == "hostonly" {
		devices, err := virtualizers.HostDevices()
		if err != nil {
			return err
		}
		hostDevice := false
		for _, device := range devices {
			if device == c.NetworkDevice {
				hostDevice = true
			}
		}
		if !hostDevice {
			return fmt.Errorf("host device %s does not exist on virtualbox", c.NetworkDevice)
		}
	}

	return nil
}

// Create creates a virtualizer using the provided manager
func Create(mgr *virtualizers.Manager, name string, headless bool, ntype string, ndevice string) error {
	c := new(Config)
	c.Headless = headless
	c.NetworkType = ntype
	c.NetworkDevice = ndevice
	err := mgr.CreateVirtualizer(name, VirtualizerID, c.Marshal())
	if err != nil {
		return err
	}
	return nil
}
