package ext4

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/vorteil/vorteil/pkg/vio"
)

const (
	InodeMaximumInlineBytes = 60
)

const (
	InodeTypeDirectory                 = 0x4000
	InodeTypeRegularFile               = 0x8000
	InodeTypeSymlink                   = 0xA000
	InodeTypeMask                      = 0xF000
	InodePermissionsMask               = 0777
	DefaultInodePermissions            = 0700
	InodeDefaultDirectoryPermissions   = InodeTypeDirectory | DefaultInodePermissions
	InodeDefaultRegularFilePermissions = InodeTypeRegularFile | DefaultInodePermissions
	InodeDefaultSymlinkPermissions     = InodeTypeSymlink | DefaultInodePermissions
)

const (
	ExtentMagic      = 0xF30A
	Ext4IndexFL      = 0x00001000 // EXT4_INDEX_FL
	Ext4ExtentsFL    = 0x00080000 // EXT4_EXTENTS_FL
	Ext4EAInodeFL    = 0x00200000 // EXT4_EA_INODE_FL
	Ext4InlineDataFL = 0x10000000 // EXT4_INLINE_DATA_FL
)

type node struct {
	node    *vio.TreeNode
	start   int64
	content uint32
	fs      uint32
}

type Inode struct {
	Permissions      uint16   // 0x0
	UID              uint16   // 0x2
	SizeLower        uint32   // 0x4
	LastAccessTime   uint32   // 0x8
	CreationTime     uint32   // 0xC
	ModificationTime uint32   // 0x10
	DeletionTime     uint32   // 0x14
	GID              uint16   // 0x18
	Links            uint16   // 0x1A
	Sectors          uint32   // 0x1C
	Flags            uint32   // 0x20
	OSV              uint32   // 0x24
	Block            [60]byte // 0x28
	GenNo            uint32   // 0x64
	FileACL          uint32   // 0x68
	SizeUpper        uint32   // 0x6C
	FragAddr         uint32   // 0x70
	OSStuff          [12]byte // 0x74
} // 0x80

func iblockInline(n *node) []byte {

	f := n.node.File
	if f.IsSymlink() {
		if f.SymlinkIsCached() {
			return []byte(f.Symlink())
		}
		panic(errors.New("tried to inline uncached symlink"))
	}

	panic(errors.New("tried to inline non-symlink"))

}

type extent struct {
	beginning int64
	length    int64
}

type ExtentHeader struct {
	Magic      uint16
	Entries    uint16
	Max        uint16
	Depth      uint16
	Generation uint32
}

type ExtentIndex struct {
	Block  uint32
	LeafLo uint32
	LeafHi uint16
	Unused uint16
}

type Extent struct {
	Block   uint32
	Len     uint16
	StartHi uint16
	StartLo uint32
}

func extentTree(extents []extent, max int64) *bytes.Buffer {

	l := len(extents)
	buf := new(bytes.Buffer)

	hdr := &ExtentHeader{
		Magic:   ExtentMagic,
		Entries: uint16(l),
		Max:     uint16(max),
	}

	err := binary.Write(buf, binary.LittleEndian, hdr)
	if err != nil {
		panic(err)
	}

	var block uint32
	for i := 0; i < l; i++ {
		e := &Extent{
			Block:   block,
			Len:     uint16(extents[i].length),
			StartLo: uint32(extents[i].beginning),
		}
		block += uint32(extents[i].length)
		err = binary.Write(buf, binary.LittleEndian, e)
		if err != nil {
			panic(err)
		}
	}

	return buf

}

func extentArray(n *node, mapper contentMapper) []extent {

	start := n.start
	length := n.content

	remainder := int64(length)
	cursor := start
	cursor += int64(n.fs - n.content)
	var extents []extent

	for remainder > 0 {
		addr, max := mapper.mapContent(cursor)
		delta := max
		if delta > remainder {
			delta = remainder
		}
		for delta > 0 {
			chunk := delta
			if chunk > 32768 {
				chunk = 32768
			}
			extents = append(extents, extent{
				beginning: addr,
				length:    chunk,
			})
			cursor += chunk
			remainder -= chunk
			delta -= chunk
		}
	}

	return extents

}

func numberOfExtents(n *node, mapper contentMapper) int64 {
	extents := extentArray(n, mapper)
	return int64(len(extents))
}

func iblockExtents(n *node, mapper contentMapper) []byte {

	extents := extentArray(n, mapper)
	max := int64(4)

	if int64(len(extents)) > max {
		panic(fmt.Sprintf("oopsie woopsie, we need to support more fragmented files: inode %d, %d extents (fs content %d %d)", n.node.NodeSequenceNumber, len(extents), n.fs, n.content))
	}

	return extentTree(extents, max).Bytes()

}

func extentsBlock(n *node, mapper contentMapper) []byte {

	extents := extentArray(n, mapper)
	max := int64((BlockSize - 12) / 12)

	if int64(len(extents)) > max {
		panic("oopsie woopsie, we need to support waaay more fragmented files")
	}

	buf := extentTree(extents, max)
	growToBlock(buf)
	return buf.Bytes()

}

func iblockExtentsRoot(n *node, mapper contentMapper) []byte {

	buf := new(bytes.Buffer)

	hdr := &ExtentHeader{
		Magic:   ExtentMagic,
		Entries: 1,
		Max:     4,
		Depth:   1,
	}

	err := binary.Write(buf, binary.LittleEndian, hdr)
	if err != nil {
		panic(err)
	}

	addr, _ := mapper.mapContent(n.start)

	idx := &ExtentIndex{
		LeafLo: uint32(addr),
	}
	err = binary.Write(buf, binary.LittleEndian, idx)
	if err != nil {
		panic(err)
	}

	return buf.Bytes()

}

func iblock(n *node, mapper contentMapper) []byte {

	f := n.node.File
	if n.fs == 0 && f.IsSymlink() && f.Size() < InodeMaximumInlineBytes {
		return iblockInline(n)
	}

	if n.fs > n.content {
		// deep extent tree
		return iblockExtentsRoot(n, mapper)
	}

	return iblockExtents(n, mapper)

}

func generateInode(n *node, mapper contentMapper) *Inode {

	inode := &Inode{}
	if n == nil {
		return inode
	}

	f := n.node.File

	inode.Permissions = InodeDefaultRegularFilePermissions
	inode.UID = SuperUID
	inode.SizeLower = uint32(f.Size())
	inode.GID = SuperGID
	inode.Links = uint16(n.node.Links)
	inode.Sectors = n.fs * SectorsPerBlock
	inode.Flags = Ext4ExtentsFL

	if f.IsSymlink() {
		inode.Permissions = InodeDefaultSymlinkPermissions
		if f.SymlinkIsCached() && f.Size() < InodeMaximumInlineBytes {
			inode.Flags &^= Ext4ExtentsFL
			// inode.Flags |= Ext4InlineDataFL
			// inode.Flags |= Ext4EAInodeFL
		}
	}

	if f.IsDir() {
		inode.Permissions = InodeDefaultDirectoryPermissions
		inode.SizeLower = uint32(n.content * BlockSize)
		if n.content > 1 {
			inode.Flags |= Ext4IndexFL
		}
	}

	copy(inode.Block[:], iblock(n, mapper))

	return inode

}
