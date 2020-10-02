package imagetools

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// MDSumImageFile ...
func MDSumImageFile(vorteilImagePath string, imageFilePath string, seekOS bool) (string, error) {
	var rdr io.Reader
	var err error
	var md5sumOut string

	vorteilImage, err := vdecompiler.Open(vorteilImagePath)
	if err != nil {
		return md5sumOut, err
	}

	defer vorteilImage.Close()

	if seekOS {
		imageFilePath = strings.TrimPrefix(imageFilePath, "/")
		rdr, err = vorteilImage.KernelFile(imageFilePath)
	} else {
		ino, err := vorteilImage.ResolvePathToInodeNo(imageFilePath)
		if err != nil {
			return md5sumOut, err
		}

		inode, err := vorteilImage.ResolveInode(ino)
		if err == nil {
			if vdecompiler.InodeIsDirectory(inode) {
				err = fmt.Errorf("\"%s\" is not a regular file", imageFilePath)
			} else {
				rdr, err = vorteilImage.InodeReader(inode)
			}
		}

		if err != nil {
			return md5sumOut, err
		}

		rdr = io.LimitReader(rdr, int64(vdecompiler.InodeSize(inode)))
	}

	hasher := md5.New()
	_, err = io.Copy(hasher, rdr)
	if err == nil {
		md5sumOut = hex.EncodeToString(hasher.Sum(nil))
	}

	return md5sumOut, err
}
