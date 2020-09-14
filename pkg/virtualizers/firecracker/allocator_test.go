package firecracker

import (
	"reflect"
	"testing"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/virtualizers"
)

func TestRegister(t *testing.T) {
	virtualizers.Register(VirtualizerID, Allocator)
	alloc := virtualizers.RegisteredVirtualizers()
	if alloc[VirtualizerID] == nil {
		t.Errorf("registering virtualizer failed, as map lookup returned nil")
	}
}

func TestAlloc(t *testing.T) {
	virt := Allocator.Alloc()
	if virt == nil {
		t.Errorf("attempting to alloc virtualizer ended up in getting nil object")
	}
}

func TestDiskAlignment(t *testing.T) {
	size := 2 * vcfg.MiB
	align := Allocator.DiskAlignment()

	if align != size {
		t.Errorf("disk alignment does not match expected %v but got %v", size, align)
	}
}

func TestDiskFormat(t *testing.T) {
	format := Allocator.DiskFormat()
	exactFormat := vdisk.RAWFormat
	if format != exactFormat {
		t.Errorf("disk format does not match %v got %v instead", exactFormat, format)
	}
}

func TestIsAvailable(t *testing.T) {
	available := Allocator.IsAvailable()

	tt := reflect.TypeOf(available)
	if tt != reflect.TypeOf(true) {
		t.Errorf("Is available didn't return a 'bool' but returned '%s'", tt)
	}
}
