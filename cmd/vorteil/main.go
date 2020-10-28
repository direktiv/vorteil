package main

import (
	"github.com/vorteil/vorteil/pkg/cli"
	"github.com/vorteil/vorteil/pkg/elog"
)

var log elog.View

func main() {
	cli.InitializeCommands()

	err := cli.RootCommand.Execute()
	if err != nil {
		cli.SetError(err, 1)
	}

	cli.HandleErrors()

}
