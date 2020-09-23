package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vproj"
)

var projectsCmd = &cobra.Command{
	Use: "projects",
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
				log.Errorf("%v", err)
				os.Exit(1)
			}
		}

		// Create Import Operation
		importOperation, err := vproj.NewImportSharedObject(projectPath, flagExcludeDefault, log)

		if err != nil {
			log.Errorf("%v", err)
			os.Exit(2)
		}

		// Start Import Operation
		if err = importOperation.Start(); err != nil {
			log.Errorf("%v", err)
			os.Exit(3)
		}
	},
}

func init() {
	f := importSharedObjectsCmd.Flags()
	f.BoolVarP(&flagExcludeDefault, "no-defaults", "e", false, "exclude default shared objects")
}
