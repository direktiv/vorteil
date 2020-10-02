/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package imagetools

import (
	"path/filepath"

	"github.com/vorteil/vorteil/pkg/ext"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// DUImageReport Returns a Disk usage report
type DUImageReport struct {
	FreeSpace  int
	ImageFiles []duImageInfo
}

type duImageInfo struct {
	FilePath string
	FileSize int
}

// DUImageFile returns the disk usage calculations of a path (imageFilePath) in a vorteilImage.
//	Disk usuage is recursive will be calculated at a depth set to maxDepth.
func DUImageFile(vorteilImage *vdecompiler.IO, imageFilePath string, calcFreeSpace bool, maxDepth int, all bool) (DUImageReport, error) {
	var duOut DUImageReport
	var depth = 0

	var recurse func(*ext.Inode, string) (int, error)
	recurse = func(inode *ext.Inode, name string) (int, error) {

		depth++
		defer func() {
			depth--
		}()

		var size int
		size = int(inode.Sectors) * ext.SectorSize

		if !vdecompiler.InodeIsDirectory(inode) {
			return size, nil
		}

		entries, err := vorteilImage.Readdir(inode)
		if err != nil {
			return 0, err
		}

		var delta int
		for i := 2; i < len(entries); i++ {
			entry := entries[i]
			child := filepath.ToSlash(filepath.Join(name, entry.Name))

			cinode, err := vorteilImage.ResolveInode(entry.Inode)
			if err != nil {
				return 0, err
			}

			delta, err = recurse(cinode, child)
			if err != nil {
				return 0, err
			}
			if all || vdecompiler.InodeIsDirectory(inode) {
				if (maxDepth >= 0 && depth <= maxDepth) || maxDepth < 0 {
					duOut.ImageFiles = append(duOut.ImageFiles, duImageInfo{
						FilePath: child,
						FileSize: delta,
					})
				}
			}
			size += delta
		}

		return size, nil
	}

	ino, err := vorteilImage.ResolvePathToInodeNo(imageFilePath)
	if err != nil {
		return duOut, err
	}

	inode, err := vorteilImage.ResolveInode(ino)
	if err != nil {
		return duOut, err
	}

	size, err := recurse(inode, imageFilePath)
	if err != nil {
		return duOut, err
	}

	duOut.ImageFiles = append(duOut.ImageFiles, duImageInfo{
		FilePath: imageFilePath,
		FileSize: size,
	})

	if calcFreeSpace {
		sb, err := vorteilImage.Superblock(0)
		if err == nil {
			duOut.FreeSpace = int(sb.UnallocatedBlocks) * int(1024<<sb.BlockSize)
		}
	}

	return duOut, err
}
