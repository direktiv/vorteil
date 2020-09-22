package ext

import (
	"errors"
	"path"

	"github.com/vorteil/vorteil/pkg/vio"
)

const (
	SectorSize               = 512
	BlockSize                = 0x1000
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

	SuperUID                    = 1000
	SuperGID                    = 1000
	inodeDirectoryPermissions   = 0x4000 | 0700
	inodeRegularFilePermissions = 0x8000 | 0700
	inodeSymlinkPermissions     = 0xA000 | 0700
)

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
	Reserved         [2]uint32
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
	i -= 1
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
	i -= 1
	a = i / ((p+1)*p + 1)
	b = i % ((p+1)*p + 1)
	if b == 0 {
		return 2
	}

	// check if the block is a first-level indirect from the third indirect
	b -= 1
	a = b / (p + 1)
	b = b % (p + 1)
	if a < p && b == 0 {
		return 1
	}

	// it must be a data block from the third indirect
	return 0

}

func calculateSymlinkSize(f vio.File) (content int64, fs int64) {
	l := int64(f.Size())
	content = divide(l, BlockSize)
	fs = calculateNumberOfIndirectBlocks(content)
	fs += content
	return content, fs
}

func calculateRegularFileSize(f vio.File) (int64, int64) {
	l := int64(f.Size())
	content := divide(l, BlockSize)
	fs := calculateNumberOfIndirectBlocks(content)
	fs += content
	return content, fs
}

func calculateDirectorySize(n *vio.TreeNode) (int64, int64) {

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

	content := divide(length, BlockSize)
	fs := calculateNumberOfIndirectBlocks(content)
	fs += content

	return content, fs

}
