package imageutil

import (
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// Format gathers Image file format information.
func Format(log elog.View, args []string) error {
	img := args[0]

	iio, err := vdecompiler.Open(img)
	if err != nil {
		return err
	}
	defer iio.Close()

	format, err := iio.ImageFormat()
	if err != nil {
		return err
	}

	log.Printf("Image file format: %s", format)
	return nil
}
