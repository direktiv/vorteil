package imageUtils

import (
	"bytes"
	"io"
	"strings"

	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// CatImageFile ...
func CatImageFile(vorteilImagePath string, imageFilePath string, os bool) (string, error) {
	var catOut string

	iio, err := vdecompiler.Open(vorteilImagePath)
	if err != nil {
		return catOut, err
	}
	defer iio.Close()

	var rdr io.Reader

	if os {
		imageFilePath = strings.TrimPrefix(imageFilePath, "/")
		rdr, err = iio.KernelFile(imageFilePath)
		if err != nil {
			return catOut, err
		}
	} else {
		ino, err := iio.ResolvePathToInodeNo(imageFilePath)
		if err != nil {
			return catOut, err
		}

		inode, err := iio.ResolveInode(ino)
		if err != nil {
			return catOut, err
		}

		if !inode.IsRegularFile() {
			return catOut, err
		}

		rdr, err = iio.InodeReader(inode)
		if err != nil {
			return catOut, err
		}

		rdr = io.LimitReader(rdr, int64(inode.Fullsize()))
	}

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, rdr)
	if err == nil {
		catOut = buf.String()
	}
	return catOut, err

}
