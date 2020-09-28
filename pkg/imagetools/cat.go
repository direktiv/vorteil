package imagetools

import (
	"fmt"
	"io"
	"strings"

	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// CatImageFile ...
func CatImageFile(vorteilImage *vdecompiler.IO, imageFilePath string, os bool) (io.Reader, error) {
	var rdr io.Reader
	var err error

	if os {
		imageFilePath = strings.TrimPrefix(imageFilePath, "/")
		rdr, err = vorteilImage.KernelFile(imageFilePath)
	} else {
		ino, err := vorteilImage.ResolvePathToInodeNo(imageFilePath)
		if err != nil {
			return nil, err
		}

		inode, err := vorteilImage.ResolveInode(ino)
		if err == nil {
			if !inode.IsRegularFile() {
				err = fmt.Errorf("\"%s\" is not a regular file", imageFilePath)
			}
		}

		if err != nil {
			return nil, err
		}

		rdr, err = vorteilImage.InodeReader(inode)
		if err != nil {
			return nil, err
		}

		rdr = io.LimitReader(rdr, int64(inode.Fullsize()))
	}

	return rdr, err

}
