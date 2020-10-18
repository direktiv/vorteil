package ext

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"path"

	"github.com/vorteil/vorteil/pkg/vio"
)

// Various ext2 build constants.
const (
	Signature                = 0xEF53
	SectorSize               = 512
	BlockSize                = 0x1000
	SuperblockOffset         = 1024
	InodeSize                = 128
	InodesPerBlock           = BlockSize / InodeSize
	BlockGroupDescriptorSize = 32
	blocksPerSuperblock      = 1
	blocksPerBlockBitmap     = 1
	blocksPerInodeBitmap     = 1

	pointerSize       = 4
	maxDirectPointers = 12
	pointersPerBlock  = BlockSize / pointerSize

	dentryNameAlignment = 4

	RootDirInode = 2

	InodeTypeDirectory          = 0x4000
	InodeTypeRegularFile        = 0x8000
	InodeTypeSymlink            = 0xA000
	InodeTypeMask               = 0xF000
	InodePermissionsMask        = 0777
	DefaultInodePermissions     = 0700
	SuperUID                    = 1000
	SuperGID                    = 1000
	inodeDirectoryPermissions   = InodeTypeDirectory | DefaultInodePermissions
	inodeRegularFilePermissions = InodeTypeRegularFile | DefaultInodePermissions
	inodeSymlinkPermissions     = InodeTypeSymlink | DefaultInodePermissions
)

// Superblock is the structure of a superblock as written to the disk.
type Superblock struct {
	TotalInodes         uint32
	TotalBlocks         uint32
	ReservedBlocks      uint32
	UnallocatedBlocks   uint32
	UnallocatedInodes   uint32
	SuperblockNumber    uint32
	BlockSize           uint32
	FragmentSize        uint32
	BlocksPerGroup      uint32
	FragmentsPerGroup   uint32
	InodesPerGroup      uint32
	LastMountTime       uint32
	LastWrittenTime     uint32
	MountsSinceCheck    uint16
	MountsCheckInterval uint16
	Signature           uint16
	State               uint16
	ErrorProtocol       uint16
	VersionMinor        uint16
	TimeLastCheck       uint32
	TimeCheckInterval   uint32
	OS                  uint32
	VersionMajor        uint32
	SuperUser           uint16
	SuperGroup          uint16
}

// BlockGroupDescriptorTableEntry is the structure of an ext block group
// descriptor table entry.
type BlockGroupDescriptorTableEntry struct {
	BlockBitmapBlockAddr uint32
	InodeBitmapBlockAddr uint32
	InodeTableBlockAddr  uint32
	UnallocatedBlocks    uint16
	UnallocatedInodes    uint16
	Directories          uint16
	_                    [14]byte
}

// Inode is the structure of an inode as written to the disk.
type Inode struct {
	Permissions      uint16
	UID              uint16
	SizeLower        uint32
	LastAccessTime   uint32
	CreationTime     uint32
	ModificationTime uint32
	DeletionTime     uint32
	GID              uint16
	Links            uint16
	Sectors          uint32
	Flags            uint32
	OSV              uint32
	DirectPointer    [12]uint32
	SinglyIndirect   uint32
	DoublyIndirect   uint32
	TriplyIndirect   uint32
	GenNo            uint32
	FileACL          uint32
	SizeUpper        uint32
	FragAddr         uint32
	OSStuff          [12]byte
}

func divide(a, b int64) int64 {
	return (a + b - 1) / b
}

func align(a, b int64) int64 {
	return divide(a, b) * b
}

func calculateNumberOfIndirectBlocks(b int64) int64 {

	var single, double, triple, p int64
	p = BlockSize / pointerSize
	single = maxDirectPointers
	double = single + p
	triple = double + p*p
	bounds := triple + p*p*p

	switch {
	case b <= single:
		return 0
	case b <= double:
		return 1
	case b <= triple:
		return 1 + 1 + divide(b-double, p)
	case b <= bounds:
		return 1 + 1 + p + 1 + divide(divide(b-triple, p), p) + divide(b-triple, p)
	default:
		panic(errors.New("file too large for ext2"))
	}

}

func blockType(i int64) int {

	var p, a, b int64
	p = BlockSize / pointerSize

	// check if the block is in the direct pointers region
	i -= maxDirectPointers
	if i < 0 {
		return 0
	}

	// check if the block is the first indirect block
	if i == 0 {
		return 1
	}

	// check if the block is a data block from the first indirect
	i -= (p + 1)
	if i < 0 {
		return 0
	}

	// check if the block is the second indirect block
	if i == 0 {
		return 2
	}

	// check if the block is a first-level indirect from the second indirect
	i--
	a = i / (p + 1)
	b = i % (p + 1)
	if a < p && b == 0 {
		return 1
	}

	// check if the block is a data block from the second indirect
	i -= (p + 1) * p
	if i < 0 {
		return 0
	}

	// check if the block is the third indirect block
	if i == 0 {
		return 3
	}

	// check if the block is a second-level indirect from the third indirect
	i--
	a = i / ((p+1)*p + 1)
	b = i % ((p+1)*p + 1)
	if b == 0 {
		return 2
	}

	// check if the block is a first-level indirect from the third indirect
	b--
	a = b / (p + 1)
	b = b % (p + 1)
	if a < p && b == 0 {
		return 1
	}

	// it must be a data block from the third indirect
	return 0

}

func calculateBlocksFromSize(size int64) (content int64, fs int64) {
	content = divide(size, BlockSize)
	fs = calculateNumberOfIndirectBlocks(content)
	fs += content
	return content, fs
}

func calculateSymlinkBlocks(f vio.File) (content int64, fs int64) {
	return calculateBlocksFromSize(int64(f.Size()))
}

func calculateRegularFileBlocks(f vio.File) (int64, int64) {
	return calculateBlocksFromSize(int64(f.Size()))
}

func calculateDirectoryBlocks(n *vio.TreeNode) (int64, int64) {

	var length, leftover int64
	length = 24 // '.' entry + ".." entry
	leftover = BlockSize - length

	for i, child := range n.Children {

		name := path.Base(child.File.Name())
		l := 8 + align(int64(len(name)+1), dentryNameAlignment)

		if leftover >= l {
			length += l
			leftover -= l
		} else {
			length += leftover
			length += l
			leftover = BlockSize - l
		}

		if leftover < 8 || i == len(n.Children)-1 {
			length += leftover
			leftover = BlockSize
		}

	}

	return calculateBlocksFromSize(length)

}

type dirTuple struct {
	name  string
	inode uint32
}

func generateDirectoryData(node *nodeBlocks) (io.Reader, error) {

	var tuples []*dirTuple
	tuples = append(tuples, &dirTuple{name: ".", inode: uint32(node.node.NodeSequenceNumber)})
	tuples = append(tuples, &dirTuple{name: "..", inode: uint32(node.node.Parent.NodeSequenceNumber)})

	for _, child := range node.node.Children {
		tuples = append(tuples, &dirTuple{name: path.Base(child.File.Name()), inode: uint32(child.NodeSequenceNumber)})
	}

	buf := new(bytes.Buffer)
	length := int64(0)
	leftover := int64(BlockSize)

	for i, child := range tuples {
		l := 8 + align(int64(len(child.name)+1), dentryNameAlignment)

		if leftover >= l && (leftover-l == 0 || leftover-l > 8) {
			length += l
			leftover -= l
		} else {
			// add a null entry into the leftover space
			_ = binary.Write(buf, binary.LittleEndian, uint32(0))        // inode
			_ = binary.Write(buf, binary.LittleEndian, uint16(leftover)) // entry size
			_ = binary.Write(buf, binary.LittleEndian, uint16(0))        // name length
			_, _ = buf.Write(bytes.Repeat([]byte{0}, int(leftover-8)))   // padding

			length += leftover
			length += l
			leftover = int64(BlockSize) - l
		}

		if leftover < 8 || i == len(tuples)-1 {
			l += leftover
			length += leftover
			leftover = int64(BlockSize)
		}

		_ = binary.Write(buf, binary.LittleEndian, child.inode)                      // inode
		_ = binary.Write(buf, binary.LittleEndian, uint16(l))                        // entry size
		_ = binary.Write(buf, binary.LittleEndian, uint16(len(child.name)))          // name length
		_ = binary.Write(buf, binary.LittleEndian, append([]byte(child.name), 0))    // name
		_, _ = buf.Write(bytes.Repeat([]byte{0}, int(l-8-int64(len(child.name))-1))) // padding

	}

	_, err := io.CopyN(buf, vio.Zeroes, align(int64(buf.Len()), BlockSize)-int64(buf.Len()))
	if err != nil {
		panic(err)
	}

	return bytes.NewReader(buf.Bytes()), nil

}
