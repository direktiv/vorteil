package vdecompiler

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bytes"
	"io"

	"github.com/vorteil/vorteil/pkg/ext"
	"github.com/vorteil/vorteil/pkg/vimg"
)

type inodeReader struct {
	iio        *IO
	inode      *ext.Inode
	block      []byte
	blockNo    int
	blockAddrs []int
	eof        bool
}

func (r *inodeReader) loadNextBlock() error {

	if r.blockNo >= len(r.blockAddrs) {
		r.eof = true
		return io.EOF
	}

	addr := r.blockAddrs[r.blockNo]

	sb, err := r.iio.Superblock(0)
	if err != nil {
		return err
	}

	blockSize := int(1024 << sb.BlockSize)

	if addr == 0 {
		r.block = bytes.Repeat([]byte{0}, blockSize)
	} else {
		lba, err := r.iio.BlockToLBA(addr)
		if err != nil {
			return err
		}

		_, err = r.iio.img.Seek(int64(lba*vimg.SectorSize), io.SeekStart)
		if err != nil {
			return err
		}

		buf := new(bytes.Buffer)
		_, err = io.CopyN(buf, r.iio.img, int64(blockSize))
		if err != nil {
			return err
		}

		r.block = buf.Bytes()
	}

	return nil

}

func (r *inodeReader) Read(p []byte) (n int, err error) {

	if r.eof {
		return 0, io.EOF
	}

	for {
		if r.block == nil || len(r.block) == 0 {
			if r.block != nil {
				r.blockNo++
			}

			err = r.loadNextBlock()
			if err != nil {
				if n > 0 && err == io.EOF {
					err = nil
				}
				return n, err
			}

		}

		k := copy(p[n:], r.block)
		n += k
		r.block = r.block[k:]

		if n == len(p) {
			return n, nil
		}
	}

}

// InodeIsRegularFile returns true if the permission bits in the inode represent
// a regular file.
func InodeIsRegularFile(inode *ext.Inode) bool {
	return inode.Permissions&ext.InodeTypeRegularFile == ext.InodeTypeRegularFile
}

// InodeIsDirectory returns true if the permission bits in the inode represent
// a directory.
func InodeIsDirectory(inode *ext.Inode) bool {
	return inode.Permissions&ext.InodeTypeDirectory == ext.InodeTypeDirectory
}

// InodeIsSymlink returns true if the permission bits in the inode represent
// a symlink.
func InodeIsSymlink(inode *ext.Inode) bool {
	return inode.Permissions&ext.InodeTypeSymlink == ext.InodeTypeSymlink
}

// InodeSize returns the size of the file respresented by the inode. It is
// safer to use this than to use the size fields directly because different
// versions of ext might have upper and lower bits stored separately that need
// combining.
func InodeSize(inode *ext.Inode) int64 {
	return (int64(inode.SizeUpper) << 32) + int64(inode.SizeLower)
}

// InodePermissionsString returns a string-representation of an inode's
// permissions modelled off the string you see with `ls -l`, e.g. `drwxr-x---`.
func InodePermissionsString(inode *ext.Inode) string {

	mode := []byte("----------")

	if InodeIsDirectory(inode) {
		mode[0] = 'd'
	} else if InodeIsSymlink(inode) {
		mode[0] = 'l'
	}

	modeChars := []byte{'r', 'w', 'x'}
	for i := 0; i < 9; i++ {
		if (inode.Permissions & (1 << (8 - i))) > 0 {
			mode[1+i] = modeChars[i%3]
		}
	}

	return string(mode)

}
