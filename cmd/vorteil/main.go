package main

import (
	"github.com/vorteil/vorteil/pkg/cli"
	"github.com/vorteil/vorteil/pkg/elog"
)

var log elog.View

func main() {
	// logger := &elog.CLI{}
	// log = logger

	// gcpCFG := google.Config{
	// 	Bucket: "GCP BUCKET",
	// 	Key:    "GCP KEY",
	// }

	// gcpData, errZ := json.Marshal(gcpCFG)
	// if errZ != nil {
	// 	panic(errZ)
	// }

	// googleProvisioner, errZ := registry.NewProvisioner("google", log, gcpData)
	// if errZ != nil {
	// 	panic(errZ)
	// }

	// googleProvisioner.Provision(&provisioners.ProvisionArgs{})
	// os.Exit(1)
	cli.InitializeCommands()

	err := cli.RootCommand.Execute()
	if err != nil {
		cli.SetError(err, 1)
	}

	cli.HandleErrors()

}
