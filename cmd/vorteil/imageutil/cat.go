package imageutil

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// Cat writes to stddout the img
func Cat(args []string, flagOS bool) error {
	img := args[0]

	iio, err := vdecompiler.Open(img)
	if err != nil {
		return err
	}
	defer iio.Close()

	for i := 1; i < len(args); i++ {
		fpath := args[i]
		var rdr io.Reader

		if flagOS {
			fpath = strings.TrimPrefix(fpath, "/")
			rdr, err = iio.KernelFile(fpath)
			if err != nil {
				return err
			}
		} else {
			ino, err := iio.ResolvePathToInodeNo(fpath)
			if err != nil {
				return err
			}

			inode, err := iio.ResolveInode(ino)
			if err != nil {
				return err
			}

			if !inode.IsRegularFile() {
				return fmt.Errorf("\"%s\" is not a regular file", fpath)
			}

			rdr, err = iio.InodeReader(inode)
			if err != nil {
				return err
			}

			rdr = io.LimitReader(rdr, int64(inode.Fullsize()))
		}

		_, err := io.Copy(os.Stdout, rdr)
		if err != nil {
			return err
		}
	}
	return nil
}
