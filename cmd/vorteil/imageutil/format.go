package imageutil

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// FormatCMD returns what the disk format is
var FormatCMD = &cobra.Command{
	Use:   "format IMAGE",
	Short: "Image file format information.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		img := args[0]

		iio, err := vdecompiler.Open(img)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		format, err := iio.ImageFormat()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		log.Printf("Image file format: %s", format)
	},
}
