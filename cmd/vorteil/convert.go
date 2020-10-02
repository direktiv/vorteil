package main

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vconvert"
)

func init() {
	f := convertContainerCmd.Flags()
	f.StringP("user", "u", "", "container registry user")
	f.StringP("password", "p", "", "container registry password")
	f.StringP("config", "c", "", "container registry configuration list")
}

var convertContainerCmd = &cobra.Command{
	Use:   "convert-container REPO:APP DESTFOLDER",
	Args:  cobra.ExactValidArgs(2),
	Short: "Convert containers into vorteil.io virtual machines",
	Long: `Convert containers into vorteil.io project folders. This command can convert
containers from a remote repository as well as from local container runtimes.
At the moment docker and containerd are supported.

Local conversion examples:

vorteil projects convert-container local.docker/nginx /target/directory
vorteil projects convert-container local.containerd/docker.io/library/tomcat:latest /target/directory

Remote conversion examples:

./vorteil projects convert-container --config=/vconvert.yaml nginx /tmp/nginx

The config file provided maps remote repository names to urls. If no file is provided
docker.io, mcr.microsoft.com and gcr.io are automatically added. The following is an example
config yaml:

repositories:
  myrepo:
   url: https://myurl
`,
	Run: func(cmd *cobra.Command, args []string) {
		// in case of an error we pass empty user/pwd/config in
		user, _ := cmd.Flags().GetString("user")
		pwd, _ := cmd.Flags().GetString("password")
		config, _ := cmd.Flags().GetString("config")

		cc, err := vconvert.NewContainerConverter(args[0], config, log)
		if err != nil {
			SetError(err, 1)
			return
		}

		err = cc.ConvertToProject(args[1], user, pwd)
		if err != nil {
			SetError(err, 2)
			return
		}
	},
}
