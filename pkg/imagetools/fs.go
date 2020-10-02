package imagetools

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"time"

	"github.com/vorteil/vorteil/pkg/vdecompiler"
	"github.com/vorteil/vorteil/pkg/vimg"
)

// FSFileReport : Contains information that summarizes a file-systems metadata
type FSFileReport struct {
	FirstLBA        int
	LastLBA         int
	Type            string
	BlockSize       int
	BlocksAllocated int
	BlocksAvaliable int
	BlockGroups     int
	MaxBlock        int
	InodesAllocated int
	InodesAvaliable int
	MaxInodes       int
	LastMountTime   time.Time
	LastWriteTime   time.Time
}

// FSImageFile Returns a summary of a vorteil image's file-system's metadata
func FSImageFile(vorteilImage *vdecompiler.IO) (FSFileReport, error) {
	var fsOut FSFileReport

	entry, err := vorteilImage.GPTEntry(vdecompiler.UTF16toString(vimg.RootPartitionName))
	if err != nil {
		return fsOut, err
	}

	sb, err := vorteilImage.Superblock(0)
	if err != nil {
		return fsOut, err
	}

	fsOut.FirstLBA = int(entry.FirstLBA)
	fsOut.LastLBA = int(entry.LastLBA)
	fsOut.Type = "ext2"
	fsOut.BlockSize = 1024 << int(sb.BlockSize)
	fsOut.BlocksAllocated = int(sb.TotalBlocks - sb.UnallocatedBlocks)
	fsOut.BlocksAvaliable = int(sb.TotalBlocks)
	fsOut.BlockGroups = int((sb.TotalBlocks + sb.BlocksPerGroup - 1) / sb.BlocksPerGroup)
	fsOut.MaxBlock = int(sb.BlocksPerGroup)
	fsOut.InodesAllocated = int(sb.TotalInodes - sb.UnallocatedInodes)
	fsOut.InodesAvaliable = int(sb.TotalInodes)
	fsOut.MaxInodes = int(sb.InodesPerGroup)
	fsOut.LastMountTime = time.Unix(int64(sb.LastMountTime), 0)
	fsOut.LastWriteTime = time.Unix(int64(sb.LastWrittenTime), 0)

	return fsOut, nil
}
