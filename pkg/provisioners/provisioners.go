package provisioners

import (
	"context"
	"fmt"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vio"
)

// Provisioner ...
type Provisioner interface {
	Type() string
	DiskFormat() vdisk.Format
	SizeAlign() vcfg.Bytes
	Provision(args *ProvisionArgs) error
	Marshal() ([]byte, error)
}

// ProvisionArgs ...
type ProvisionArgs struct {
	Name            string
	Description     string
	Force           bool
	ReadyWhenUsable bool
	Context         context.Context
	Image           vio.File
}

type InvalidProvisionerError struct {
	Err error
}

func (e *InvalidProvisionerError) Error() string {
	return fmt.Sprintf("provisioner is invalid: %v", e.Err)
}

func init() {

}
