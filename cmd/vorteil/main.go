package main

import (
	"encoding/json"
	"io"

	"github.com/sirupsen/logrus"
	"github.com/vorteil/vorteil/pkg/provisioners/registry"
	"github.com/vorteil/vorteil/pkg/vcfg"

	"github.com/vorteil/vorteil/pkg/ext4"
	"github.com/vorteil/vorteil/pkg/ova"
	"github.com/vorteil/vorteil/pkg/provisioners"
	"github.com/vorteil/vorteil/pkg/provisioners/nutanix"
	"github.com/vorteil/vorteil/pkg/provisioners/vcenter"
	"github.com/vorteil/vorteil/pkg/xfs"

	"github.com/vorteil/vorteil/pkg/cli"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vio"
)

var logger elog.View

func init() {
	log := &elog.CLI{}
	logrus.SetFormatter(log)
	logrus.SetLevel(logrus.TraceLevel)
	logger = log
}

// cli.SetError() wrapper
func setError(err error, statusCode int) {
	logger.Errorf(err.Error())
	cli.SetError(err, statusCode)
}

func main() {

	defer cli.HandleErrors()

	// Init FOSS COMMANDS
	cli.InitializeCommands()

	// OVA Image Format Added
	ovaBuilder := func(w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) (io.WriteSeeker, error) {
		return ova.NewWriter(w, b, cfg)
	}

	vdisk.RegisterNewDiskFormat(ova.ImageFormatOVA, ".ova", 0x200000, 1500, ovaBuilder)

	newExt4 := func(log elog.Logger, tree vio.FileTree, args interface{}) (vimg.FSCompiler, error) {
		return ext4.NewCompiler(&ext4.CompilerArgs{
			Logger:   log,
			FileTree: tree,
		}), nil
	}

	err := vdisk.RegisterFilesystemCompiler("ext4", newExt4)
	if err != nil {
		setError(err, 1)
		return
	}

	newXFS := func(log elog.Logger, tree vio.FileTree, args interface{}) (vimg.FSCompiler, error) {
		return xfs.NewCompiler(&xfs.CompilerArgs{
			Logger:   log,
			FileTree: tree,
		}), nil
	}

	err = vdisk.RegisterFilesystemCompiler("xfs", newXFS)
	if err != nil {
		setError(err, 2)
		return
	}

	// Nutanix
	nutanixFn := func(log elog.View, data []byte) (provisioners.Provisioner, error) {
		var cfg nutanix.Config
		err := json.Unmarshal(data, &cfg)
		if err != nil {
			return nil, err
		}
		return nutanix.NewProvisioner(log, &cfg)
	}

	err = registry.RegisterProvisioner(nutanix.ProvisionerType, nutanixFn)
	if err != nil {
		setError(err, 3)
		return
	}

	cli.AddNewProvisionerCmd(nutanix.ProvisionersNewNutanixCmd)

	// VCENTER
	vcenterFn := func(log elog.View, data []byte) (provisioners.Provisioner, error) {
		var cfg vcenter.Config
		err := json.Unmarshal(data, &cfg)
		if err != nil {
			return nil, err
		}
		return vcenter.NewProvisioner(log, &cfg)
	}

	err = registry.RegisterProvisioner(vcenter.ProvisionerType, vcenterFn)
	if err != nil {
		setError(err, 4)
		return
	}

	cli.AddNewProvisionerCmd(vcenter.ProvisionersNewVCenterCmd)

	err = cli.RootCommand.Execute()
	if err != nil {
		setError(err, 5)
		return
	}

}
