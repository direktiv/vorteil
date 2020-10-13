package cli

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vpkg"
	"github.com/vorteil/vorteil/pkg/vproj"
)

var packagesCmd = &cobra.Command{
	Use:   "packages",
	Short: "Create and interact with Vorteil packages",
	Long: `Vorteil packages are compressed and optimized archives containing all of the
information needed to construct a Vorteil virtual disk vdecompiler. They generally
represent an immutable application that can be expected to operate identically
on all supported hypervisors, and they can include metadata and information that
helps to identify it and explain its purpose and its use.

The packages subcommand is a parent to all package related commands and
functionality in the CLI including creating new packages, unpacking the
contents of an existing package, and probing package files for a quick summary
of the application within.`,
}

var packCmd = &cobra.Command{
	Use:     "pack [PACKABLE]",
	Aliases: []string{"pack", "package"},
	Short:   "Create a Vorteil package",
	Long: `Create a new Vorteil package from a project. Vorteil packages are
compressed and optimized archives containing all of the information needed to
construct a Vorteil virtual disk vdecompiler. They generally represent an immutable
application that can be expected to operate identically on all supported
hypervisors, and they can include metadata and information that helps to
identify it and explain its purpose and its use.`,
	Args: cobra.RangeArgs(0, 1),
	Run: func(cmd *cobra.Command, args []string) {

		packablePath := "."
		if len(args) >= 1 {
			packablePath = args[0]
		}

		suffix := ".vorteil"
		_, base := filepath.Split(strings.TrimSuffix(filepath.ToSlash(packablePath), "/"))
		outputPath := filepath.Join(".", strings.TrimSuffix(base, suffix)+suffix)
		if flagOutput != "" {
			outputPath = flagOutput
			if !strings.HasSuffix(outputPath, suffix) {
				log.Warnf("file name does not end with '%s' file extension", suffix)
			}
		}

		err := checkValidNewFileOutput(outputPath, flagForce, "output", "-f")
		if err != nil {
			SetError(err, 1)
			return
		}

		builder, err := getPackageBuilder("PACKABLE", packablePath)
		if err != nil {
			SetError(err, 2)
			return
		}

		err = modifyPackageBuilder(builder)
		if err != nil {
			SetError(err, 3)
			return
		}

		builder.SetCompressionLevel(int(flagCompressionLevel))

		f, err := os.Create(outputPath)
		if err != nil {
			SetError(err, 5)
			return
		}
		defer f.Close()

		err = builder.Pack(f)
		if err != nil {
			SetError(err, 6)
			return
		}

		err = f.Close()
		if err != nil {
			SetError(err, 7)
			return
		}

		log.Printf("created package: %s", outputPath)
	},
}

func init() {
	f := packCmd.Flags()
	f.BoolVarP(&flagForce, "force", "f", false, "force overwrite of existing files")
	f.StringVarP(&flagOutput, "output", "o", "", "path to put package file")
	f.UintVar(&flagCompressionLevel, "compression-level", 1, "compression level (0-9)")
}

var unpackCmd = &cobra.Command{
	Use:     "unpack PACKAGE DEST",
	Aliases: []string{"extract"},
	Short:   "Unpack a Vorteil package",
	Long: `Unpack the contents of a Vorteil package into a directory. To simplify
subsequent commands the unpacked files will be organized into a new project
automatically.

The PACKAGE argument must be a path to a Vorteil package. If DEST is
provided it must be a path to a directory that is not already a Vorteil project,
or path to a file that does not exist and could be created without deleting any
other files. If the DEST argument is omitted it will default to ".".`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {

		pkgPath := args[0]
		prjPath := args[1]

		// Create project path 'prjPath' if it does not exist
		if _, err := os.Stat(prjPath); os.IsNotExist(err) {
			if err = os.Mkdir(prjPath, 0777); err != nil {
				log.Errorf("could not create DEST path \"%s\", err: %v", prjPath, err)
			} else {
				log.Debugf("created DEST path \"%s\"", prjPath)
			}
		}

		err := checkValidNewDirOutput(prjPath, flagForce, "DEST", "-f")
		if err != nil {
			SetError(err, 1)

			return
		}
		pkg, err := getPackageBuilder("PACKABLE", pkgPath)
		if err != nil {
			SetError(err, 2)
			return
		}
		defer pkg.Close()
		err = modifyPackageBuilder(pkg)
		if err != nil {
			SetError(err, 3)
			return
		}
		pkgr, err := vpkg.ReaderFromBuilder(pkg)
		if err != nil {
			SetError(err, 4)
			return
		}
		defer pkgr.Close()
		err = vproj.CreateFromPackage(prjPath, pkgr)
		if err != nil {
			SetError(err, 5)
			return
		}

		log.Printf("unpacked to: %s", prjPath)
	},
}

func init() {
	f := unpackCmd.Flags()
	f.BoolVarP(&flagForce, "force", "f", false, "force overwrite of existing files")
}
