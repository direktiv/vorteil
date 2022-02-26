package ext4

import (
	"bytes"
	"io"

	"github.com/vorteil/vorteil/pkg/vio"
)

func divide(a, b int64) int64 {
	return (a + b - 1) / b
}

func align(a, b int64) int64 {
	return divide(a, b) * b
}

func growToBlock(buf *bytes.Buffer) {

	size := align(int64(buf.Len()), BlockSize)
	if size < BlockSize {
		size = BlockSize
	}
	_, err := io.CopyN(buf, vio.Zeroes, size-int64(buf.Len()))
	if err != nil {
		panic(err)
	}

}

type contentMapper interface {
	mapContent(block int64) (addr int64, max int64)
}

func calculateBlocksFromSize(size int64) int64 {

	// NOTE: to keep things simple it is assumed here that every file can fit into four inline extent leaf nodes. Since these file-systems are minimally fragmented this should always be true except in the case of extremely large files.
	return divide(size, BlockSize)

}

func calculateSymlinkBlocks(f vio.File) int64 {

	size := int64(f.Size())

	// store small symlinks in the inode
	if size < InodeMaximumInlineBytes {
		size = 0
	}

	return calculateBlocksFromSize(size)

}

func calculateRegularFileBlocks(f vio.File) int64 {
	// NOTE: super small files cannot be stored in an inode right now because of limitations within the vio.FileTree logic when stream-building.
	// NOTE: we are not enforcing minimum file block pre-allocations on build because most files written during build are probably only for reading.
	return calculateBlocksFromSize(int64(f.Size()))
}
