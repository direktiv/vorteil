package vcenter

import (
	"bytes"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/cli"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/provisioners"
)

var (
	provisionersNewPassphrase string

	// Vsphere
	provisionersNewVCenterUsername   string
	provisionersNewVCenterPassword   string
	provisionersNewVCenterAddress    string
	provisionersNewVCenterDatacenter string
	provisionersNewVCenterDatastore  string
	provisionersNewVCenterCluster    string
	provisionersNewVCenterNotes      string
)

var log elog.View

// ProvisionersNewVCenterCmd is a cobra command that can be used to create a new
// vcenter provisioner.
var ProvisionersNewVCenterCmd = &cobra.Command{
	Use:   "vcenter <OUTPUT_FILE>",
	Short: "Add a new VCenter Provisioner.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		f, err := os.OpenFile(args[0], os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			cli.SetError(err, 1)
			return
		}
		defer f.Close()
		p, err := NewProvisioner(log, &Config{
			Username:   provisionersNewVCenterUsername,
			Password:   provisionersNewVCenterPassword,
			Address:    provisionersNewVCenterAddress,
			Datacenter: provisionersNewVCenterDatacenter,
			Datastore:  provisionersNewVCenterDatastore,
			Cluster:    provisionersNewVCenterCluster,
			Notes:      provisionersNewVCenterNotes,
		})
		if err != nil {
			cli.SetError(err, 4)
			return
		}

		data, err := p.Marshal()
		if err != nil {
			cli.SetError(err, 5)
			return
		}
		out := provisioners.Encrypt(data, provisionersNewPassphrase)
		_, err = io.Copy(f, bytes.NewReader(out))
		if err != nil {
			cli.SetError(err, 6)
			return
		}
	},
}

func init() {
	f := ProvisionersNewVCenterCmd.Flags()
	f.StringVarP(&provisionersNewVCenterDatastore, "datastore", "s", "", "Datastore name (required)")
	ProvisionersNewVCenterCmd.MarkFlagRequired("datastore")
	f.StringVarP(&provisionersNewVCenterDatacenter, "datacenter", "x", "", "Datacenter name (required)")
	ProvisionersNewVCenterCmd.MarkFlagRequired("datacenter")
	f.StringVar(&provisionersNewVCenterCluster, "host-cluster", "", "Host cluster name (required)")
	ProvisionersNewVCenterCmd.MarkFlagRequired("host-cluster")
	f.StringVarP(&provisionersNewVCenterAddress, "address", "a", "", "Address (eg. https://vcsa.example.com) (required)")
	ProvisionersNewVCenterCmd.MarkFlagRequired("address")
	f.StringVarP(&provisionersNewVCenterUsername, "username", "u", "", "VMWare username (required)")
	ProvisionersNewVCenterCmd.MarkFlagRequired("username")
	f.StringVarP(&provisionersNewVCenterPassword, "password", "p", "", "VMWare password (required)")
	ProvisionersNewVCenterCmd.MarkFlagRequired("password")
}
