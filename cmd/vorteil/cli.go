/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package main

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
	flagCompressionLevel uint
	flagForce            bool
	flagExcludeDefault   bool
	flagFormat           string
	flagOutput           string
	flagPlatform         string
	flagGUI              bool
	flagOS               bool
	flagRecord           string
	flagShell            bool
	flagTouched          bool
)

const (
	platformQEMU        = "qemu"
	platformVirtualBox  = "virtualbox"
	platformHyperV      = "hyper-v"
	platformFirecracker = "firecracker"
)

func commandInit() {

	// Here we attack VCFG modification flags to relevant commands. Because of
	// the order Go runs init functions this is the safest place to do this.
	addModifyFlags(buildCmd.Flags())
	addModifyFlags(runCmd.Flags())
	addModifyFlags(provisionCmd.Flags())

	// setup logging across all commands
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "enable verbose output")
	rootCmd.PersistentFlags().BoolVarP(&flagDebug, "debug", "d", false, "enable debug output")
	rootCmd.PersistentFlags().BoolVarP(&flagJSON, "json", "j", false, "enable json output")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {

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
	rootCmd.AddCommand(commandShortcut(versionCmd))
	rootCmd.AddCommand(commandShortcut(buildCmd))
	rootCmd.AddCommand(commandShortcut(decompileCmd))
	rootCmd.AddCommand(commandShortcut(provisionCmd))
	rootCmd.AddCommand(commandShortcut(packCmd))
	rootCmd.AddCommand(commandShortcut(unpackCmd))
	rootCmd.AddCommand(commandShortcut(convertContainerCmd))
	rootCmd.AddCommand(commandShortcut(importSharedObjectsCmd))

	// Here is the visible command structure definition.
	rootCmd.AddCommand(imagesCmd)
	rootCmd.AddCommand(packagesCmd)
	rootCmd.AddCommand(projectsCmd)
	rootCmd.AddCommand(provisionersCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(initFirecrackerCmd)

	addImagesCmd()

	packagesCmd.AddCommand(packCmd)
	packagesCmd.AddCommand(unpackCmd)

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

var rootCmd = &cobra.Command{
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
