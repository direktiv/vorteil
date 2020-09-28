package imageUtils

import (
	"io"
	"os"

	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// FSIMGImage ...
func FSIMGImage(vorteilImage *vdecompiler.IO, destPath string) error {
	f, err := os.Create(destPath)
	if err != nil {
		return err

	}
	defer f.Close()

	rdr, err := vorteilImage.PartitionReader(vdecompiler.FilesystemPartitionName)
	if err != nil {
		_ = os.Remove(f.Name())
		return err

	}

	_, err = io.Copy(f, rdr)
	if err != nil {
		_ = os.Remove(f.Name())
		return err
	}

	return nil
}
