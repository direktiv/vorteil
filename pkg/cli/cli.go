package cli

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/vorteil/vorteil/pkg/elog"
)

var log elog.View

var (
	flagJSON             bool
	flagVerbose          bool
	flagDebug            bool
	flagDefault          bool
	flagCompressionLevel uint
	flagForce            bool
	flagExcludeDefault   bool
	flagFormat           string
	flagOutput           string
	flagPlatform         string
	flagName             string
	flagKey              string
	flagGUI              bool
	flagOS               bool
	flagRecord           string
	flagShell            bool
	flagTouched          bool

	pushOrganisation string
	pushBucket       string
)

const (
	platformQEMU        = "qemu"
	platformVirtualBox  = "virtualbox"
	platformHyperV      = "hyper-v"
	platformFirecracker = "firecracker"
)

func InitializeCommands() {

	// Here we attack VCFG modification flags to relevant commands. Because of
	// the order Go runs init functions this is the safest place to do this.
	addModifyFlags(buildCmd.Flags())
	addModifyFlags(runCmd.Flags())
	addModifyFlags(provisionCmd.Flags())
	addModifyFlags(unpackCmd.Flags())
	addModifyFlags(packCmd.Flags())
	// setup logging across all commands
	RootCommand.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "enable verbose output")
	RootCommand.PersistentFlags().BoolVarP(&flagDebug, "debug", "d", false, "enable debug output")
	RootCommand.PersistentFlags().BoolVarP(&flagJSON, "json", "j", false, "enable json output")

	RootCommand.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {

		logger := &elog.CLI{}

		if flagJSON {
			logger.DisableTTY = true
			logrus.SetFormatter(&logrus.JSONFormatter{})
		} else {
			logrus.SetFormatter(logger)
		}

		logrus.SetLevel(logrus.TraceLevel)

		if flagDebug {
			logger.IsDebug = true
			logger.IsVerbose = true
		} else if flagVerbose {
			logger.IsVerbose = true
		}

		log = logger

		return nil
	}

	// Here we define some hidden top-level shortcuts.
	RootCommand.AddCommand(commandShortcut(versionCmd))
	RootCommand.AddCommand(commandShortcut(buildCmd))
	RootCommand.AddCommand(commandShortcut(decompileCmd))
	RootCommand.AddCommand(commandShortcut(provisionCmd))
	RootCommand.AddCommand(commandShortcut(packCmd))
	RootCommand.AddCommand(commandShortcut(unpackCmd))
	RootCommand.AddCommand(commandShortcut(convertContainerCmd))
	RootCommand.AddCommand(commandShortcut(importSharedObjectsCmd))

	// Here is the visible command structure definition.
	RootCommand.AddCommand(imagesCmd)
	RootCommand.AddCommand(packagesCmd)
	RootCommand.AddCommand(projectsCmd)
	RootCommand.AddCommand(provisionersCmd)
	RootCommand.AddCommand(runCmd)

	RootCommand.AddCommand(repositoriesCmd)
	// RootCommand.AddCommand(initFirecrackerCmd)

	repositoriesCmd.AddCommand(pushCmd)
	repositoriesCmd.AddCommand(keysCmd)

	keysCmd.AddCommand(createKeyCmd)
	keysCmd.AddCommand(deleteKeyCmd)
	keysCmd.AddCommand(listKeysCmd)

	addImagesCmd()

	packagesCmd.AddCommand(packCmd)
	packagesCmd.AddCommand(unpackCmd)

	projectsCmd.AddCommand(newProjectCmd)
	addModifyFlags(newProjectCmd.Flags())

	projectsCmd.AddCommand(convertContainerCmd)
	projectsCmd.AddCommand(importSharedObjectsCmd)

	provisionersCmd.AddCommand(provisionersNewCmd)

	provisionersNewCmd.AddCommand(provisionersNewAmazonEC2Cmd)
	provisionersNewCmd.AddCommand(provisionersNewAzureCmd)
	provisionersNewCmd.AddCommand(provisionersNewGoogleCmd)
}

func addImagesCmd() {
	imagesCmd.AddCommand(buildCmd)
	imagesCmd.AddCommand(decompileCmd)
	imagesCmd.AddCommand(provisionCmd)
	imagesCmd.AddCommand(catCmd)
	imagesCmd.AddCommand(cpCmd)
	imagesCmd.AddCommand(duCmd)
	imagesCmd.AddCommand(formatCmd)
	imagesCmd.AddCommand(fsCmd)
	imagesCmd.AddCommand(fsimgCmd)
	imagesCmd.AddCommand(gptCmd)
	imagesCmd.AddCommand(lsCmd)
	imagesCmd.AddCommand(md5Cmd)
	imagesCmd.AddCommand(statCmd)
	imagesCmd.AddCommand(treeCmd)
}

func commandShortcut(cmd *cobra.Command) *cobra.Command {
	c := *cmd
	c.Aliases = []string{}
	c.Hidden = true
	return &c
}

var RootCommand = &cobra.Command{
	Use:   "vorteil",
	Short: "Vorteil's command-line interface",
	Long: `Vorteil's command-line interface provides a complete set of tools for developers
to create, test, optimize, and build Vorteil apps.`,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "View CLI version information",
	Long:  "View CLI version information",
	Args:  cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {

		format, err := cmd.Flags().GetString("format")
		if err != nil {
			panic(err)
		}

		switch format {
		case "json", "", "plain":
			return nil
		default:
			return fmt.Errorf("invalid format '%s'", format)
		}
	},
	Run: func(cmd *cobra.Command, args []string) {

		format, err := cmd.Flags().GetString("format")
		if err != nil {
			panic(err)
		}

		switch format {
		case "json":
			fmt.Printf("{\n\t\"version\": \"%s\",\n\t\"ref\": \"%s\",\n\t\"released\": \"%s\"\n}\n",
				release, commit, date)
		default:
			fmt.Printf("Version: %s\nRef: %s\nReleased: %s\n", release, commit, date)
		}

	},
}

func init() {
	f := versionCmd.Flags()
	f.String("format", "", "specify output format (json, plain)")
}
