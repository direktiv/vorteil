/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package imagetools

import (
	"io"
	"os"

	"github.com/vorteil/vorteil/pkg/vdecompiler"
	"github.com/vorteil/vorteil/pkg/vimg"
)

// FSIMGImage copies a vorteil image's file system partition to destPath
func FSIMGImage(vorteilImage *vdecompiler.IO, destPath string) error {
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	rdr, err := vorteilImage.PartitionReader(vdecompiler.UTF16toString(vimg.RootPartitionName))
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
