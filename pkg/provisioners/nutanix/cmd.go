package nutanix

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

	// Nutanix
	provisionerNewNutanixUsername string
	provisionerNewNutanixPassword string
	provisionerNewNutanixHost     string
)

var log elog.View

// ProvisionersNewNutanixCmd creates a new provisioner for nutanix and saves to a file
var ProvisionersNewNutanixCmd = &cobra.Command{
	Use:   "nutanix <OUTPUT_FILE>",
	Short: "Add a new Nutanix Provisioner.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		f, err := os.OpenFile(args[0], os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			cli.SetError(err, 1)
			return
		}
		defer f.Close()

		p, err := NewProvisioner(log, &Config{
			Username: provisionerNewNutanixUsername,
			Password: provisionerNewNutanixPassword,
			Host:     provisionerNewNutanixHost,
		})
		if err != nil {
			cli.SetError(err, 2)
			return
		}

		data, err := p.Marshal()
		if err != nil {
			cli.SetError(err, 3)
			return
		}

		out := provisioners.Encrypt(data, provisionersNewPassphrase)
		_, err = io.Copy(f, bytes.NewReader(out))
		if err != nil {
			cli.SetError(err, 4)
			return
		}

	},
}

func init() {
	f := ProvisionersNewNutanixCmd.Flags()

	f.StringVarP(&provisionerNewNutanixUsername, "username", "u", "", "Username (required)")
	ProvisionersNewNutanixCmd.MarkFlagRequired("username")
	f.StringVarP(&provisionerNewNutanixPassword, "password", "p", "", "Password (required)")
	ProvisionersNewNutanixCmd.MarkFlagRequired("password")
	f.StringVarP(&provisionerNewNutanixHost, "host", "n", "", "Hostname (required)")
	ProvisionersNewNutanixCmd.MarkFlagRequired("host")

}
