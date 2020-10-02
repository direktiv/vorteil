package imagetools

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"
	"io"
	"strings"

	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// CatImageFile returns a io.Reader that reads the contents of the file at the imageFilePath inside
// 	the passed vorteilImage. If the path does not exist inside vorteilImage the reader is returned
// 	as nil accompanied by an error.
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
			if !vdecompiler.InodeIsRegularFile(inode) {
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

		rdr = io.LimitReader(rdr, int64(vdecompiler.InodeSize(inode)))
	}

	return rdr, err

}
