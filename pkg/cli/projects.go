package cli

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vproj"
)

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "Helper commands for working with Vorteil projects",
	Long: `Vorteil projects are a useful way of working with Vorteil during the development
or testing of an application. They organize the files and information needed to
produce Vorteil images, and provide makefile-like functionality where multiple
different configurations can be managed. A Vorteil project is any directory
containing a '.vorteilproject' file.

The project file is a TOML file. To be valid, the project file must define at
least one 'target', which is a configuration that completely defines a buildable
Vorteil application.

Each project target must specify a 'vcfg' that can be used to resolve the
application binary (from the vcfg's 'binary' field). Multiple vcfgs can be
specified, and they will be merged over the top of each other sequentially when
the project target is evaluated. Only one vcfg specified for a project target
needs to define a 'binary'.

Each target may optionally define a 'name' used to identify it, an 'icon' which
provides a path to a file that can be used as a package icon, and any number of
'files', which may be files or directories that should be included on the
file-system of the application.

Multiple project targets may be defined, and the first one defined in the file
will always be considered the default project target to be used unless otherwise
specified. When specifying another project target its 'name' must be used to
identify it.

In addition to targets, a project file may provide a list of ignore patterns.
By default a Vorteil application built from a project will include the entire
project directory's contents on its file-system -- this includes the project
file itself. To exclude files or directories from being automatically added to
the application's file-system, add a pattern matching it to the list of ignore
patterns. Files specifically specified for a target are not subject to any of
your ignore patterns.

All paths provided in project files can be either absolute paths or relative
paths. Any relative paths will always be interpreted as relative to the
project's directory.

Here's an example of what a project file can look like:


ignore = [".vorteilproject", "*.vcfg", "debug"]

[[target]]
	name = "debug"
	vcfgs = ["helloworld.vcfg", "debug.vcfg"]
	icon = "helloworld.png"
	files = ["debug/helloworld.conf"]

[[target]]
	name = "prod"
	vcfgs = ["helloworld.vcfg"]
	icon = "helloworld.png"


Projects are valid sources for all VCLI commands that build Vorteil images or
create Vorteil packages. For such commands, the source is assumed to be a
project if it is a path to a directory containing a '.vorteilproject' file.
Without any further information, the first target within the project file will
be used, but alternate targets can be used by appending '::<target>' to the
path.`,
	Example: `  Turn an existing directory into a project, based on binary 'helloworld':

	$ %s new helloworld

Check the validity of the project (useful if you've made changes):

	$ %s lint

Using a project as the source argument for packaging:

	$ %s pack .::prod

`,
}

var newProjectCmd = &cobra.Command{
	Use:   "new [PROJECT]",
	Short: "New creates a default.vcfg and a .vorteilproject at a certain directory",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var err error
		var projectPath string
		if len(args) != 0 {
			err = vcfgFlags.Validate()
			if err != nil {
				SetError(err, 1)
				return
			}
			projectPath, err = filepath.Abs(args[0])
			if err != nil {
				SetError(err, 2)
				return
			}
			// make sure directory is created
			err = os.MkdirAll(projectPath, os.ModePerm)
			if err != nil {
				SetError(err, 3)
				return
			}

			err = vproj.NewProject(projectPath, &overrideVCFG, log)
			if err != nil {
				SetError(err, 4)
				return
			}
		}
	},
}

var importSharedObjectsCmd = &cobra.Command{
	Use:   "import-shared-objects [PROJECT]",
	Short: "Import shared objects required by the binary targeted within the project.",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var projectPath string = "."
		var err error

		// Get Project Path
		if len(args) != 0 {
			projectPath, err = filepath.Abs(args[0])
			if err != nil {
				SetError(err, 1)
				return
			}
		}

		// Create Import Operation
		importOperation, err := vproj.NewImportSharedObject(projectPath, flagExcludeDefault, log)

		if err != nil {
			SetError(err, 2)
			return
		}

		// Start Import Operation
		if err = importOperation.Start(); err != nil {
			SetError(err, 3)
			return
		}
	},
}

func init() {
	f := importSharedObjectsCmd.Flags()
	f.BoolVarP(&flagExcludeDefault, "no-defaults", "e", false, "exclude default shared objects")
}
