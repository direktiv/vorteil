package ext4

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/ioutil"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/vorteil/vorteil/pkg/vio"
)

type testContentMapper struct {
}

func (mapper *testContentMapper) mapContent(block int64) (addr int64, max int64) {
	// simple setup where we alternate between 5 blocks available and 5 blocks reserved
	g := block / 5
	addr = (g+1)*5 + block
	max = align(addr, 10) - addr
	return
}

func offsetOf(obj, field interface{}) int {

	err := binary.Read(vio.Zeroes, binary.LittleEndian, obj)
	if err != nil {
		panic(err)
	}

	ptr := (*uint8)(unsafe.Pointer(reflect.ValueOf(field).Pointer()))
	val := *ptr
	*ptr = 0xFF

	buf := new(bytes.Buffer)
	err = binary.Write(buf, binary.LittleEndian, obj)
	if err != nil {
		panic(err)
	}

	*ptr = val
	data := buf.Bytes()

	for i, b := range data {
		if b != 0 {
			return i
		}
	}

	return 0

}

func TestInodeStruct(t *testing.T) {

	// check that the struct is the correct size
	inode := &Inode{}
	size := binary.Size(inode)

	if size != InodeSize {
		t.Errorf("struct Inode is the wrong size -- expect %d but got %d", InodeSize, size)
	}

	// check that a couple of the fields are at the correct offsets
	var offset int

	offset = offsetOf(inode, &inode.Flags)
	if offset != 32 {
		t.Errorf("struct Inode has been corrupted (a field is offset incorrectly)")
	}

	offset = offsetOf(inode, &inode.SizeUpper)
	if offset != 108 {
		t.Errorf("struct Inode has been corrupted (a field is offset incorrectly)")
	}

}

func TestGenerateUnusedInode(t *testing.T) {
	inode := generateInode(nil, nil)
	if inode == nil {
		t.Errorf("unused inodes not being allocated the correct length of space")
	}
}

func TestGenerateInodeEmptyFile(t *testing.T) {

	n := &node{
		node: &vio.TreeNode{
			File: vio.CustomFile(vio.CustomFileArgs{
				ReadCloser: ioutil.NopCloser(io.LimitReader(vio.Zeroes, 0)),
			}),
			NodeSequenceNumber: 42,
			Links:              1,
		},
	}

	inode := generateInode(n, nil)

	if inode.Permissions != InodeDefaultRegularFilePermissions {
		t.Errorf("inode has incorrect file permissions -- expect %x but got %x", InodeDefaultRegularFilePermissions, inode.Permissions)
	}

	if inode.Flags&Ext4ExtentsFL == 0 {
		t.Errorf("inode has missing Ext4ExtentsFL flag")
	}

	hdr := new(ExtentHeader)
	err := binary.Read(bytes.NewReader(inode.Block[:]), binary.LittleEndian, hdr)
	if err != nil {
		t.Error(err)
	}

	if hdr.Magic != ExtentMagic {
		t.Errorf("inode has missing extents tree")
	}

	if hdr.Entries != 0 {
		t.Errorf("inode extents tree has non-zero number of entries despite representing a zero length file")
	}

}

func TestGenerateInodeSmallFile(t *testing.T) {

	size := int64(38912) // 10 blocks
	blocks := divide(size, BlockSize)

	n := &node{
		node: &vio.TreeNode{
			File: vio.CustomFile(vio.CustomFileArgs{
				Size:       int(size),
				ReadCloser: ioutil.NopCloser(io.LimitReader(vio.Zeroes, size)),
			}),
			NodeSequenceNumber: 42,
			Links:              1,
		},
		start:   2,
		content: uint32(blocks),
		fs:      uint32(blocks),
	}

	inode := generateInode(n, &testContentMapper{})

	if inode.Permissions != InodeDefaultRegularFilePermissions {
		t.Errorf("inode has incorrect file permissions -- expect %x but got %x", InodeDefaultRegularFilePermissions, inode.Permissions)
	}

	if inode.Flags&Ext4ExtentsFL == 0 {
		t.Errorf("inode has missing Ext4ExtentsFL flag")
	}

	if int64(inode.SizeLower) != size {
		t.Errorf("inode has incorrect size -- expect %d but got %d", size, inode.SizeLower)
	}

	sectors := blocks * SectorsPerBlock
	if int64(inode.Sectors) != sectors {
		t.Errorf("inode has incorrect number of sectors -- expect %d but got %d", sectors, inode.Sectors)
	}

	iblock := bytes.NewReader(inode.Block[:])

	hdr := new(ExtentHeader)
	err := binary.Read(iblock, binary.LittleEndian, hdr)
	if err != nil {
		t.Error(err)
	}

	if hdr.Magic != ExtentMagic {
		t.Errorf("inode has missing extents tree")
	}

	if hdr.Max != 4 {
		t.Errorf("inode extents tree has bad max number of entries -- expect %d but got %d", 4, hdr.Max)
	}

	if hdr.Entries != 3 {
		t.Errorf("inode extents tree has bad number of entries -- expect %d but got %d", 3, hdr.Entries)
	}

	if hdr.Depth != 0 {
		t.Errorf("inode extents tree has bad depth -- expect %d but got %d", 0, hdr.Depth)
	}

	var extents [4]Extent
	err = binary.Read(iblock, binary.LittleEndian, &extents)
	if err != nil {
		t.Error(err)
	}

	assertExtent := func(i int, block, start, l int64) {

		extent := extents[i]

		if int64(extent.Block) != block {
			t.Errorf("inode extents tree entry %d has unexpected 'Block' value -- expect %d but got %d", i, block, extent.Block)
		}

		if int64(extent.StartLo) != start {
			t.Errorf("inode extents tree entry %d has unexpected 'StartLo' value -- expect %d but got %d", i, start, extent.StartLo)
		}

		if int64(extent.Len) != l {
			t.Errorf("inode extents tree entry %d has unexpected 'Len' value -- expect %d but got %d", i, l, extent.Len)
		}

	}

	assertExtent(0, 0, 7, 3)
	assertExtent(1, 3, 15, 5)
	assertExtent(2, 8, 25, 2)
	assertExtent(3, 0, 0, 0)

}

func TestGenerateInodeInlineSymlink(t *testing.T) {

	link := strings.Repeat("v", 50)
	size := int64(len(link))
	blocks := int64(0)

	n := &node{
		node: &vio.TreeNode{
			File: vio.CustomFile(vio.CustomFileArgs{
				Size:               int(size),
				ReadCloser:         ioutil.NopCloser(strings.NewReader(link)),
				IsSymlink:          true,
				IsSymlinkNotCached: false,
				Symlink:            link,
			}),
			NodeSequenceNumber: 42,
			Links:              1,
		},
	}

	inode := generateInode(n, &testContentMapper{})

	if inode.Permissions != InodeDefaultSymlinkPermissions {
		t.Errorf("inode has incorrect file permissions -- expect %x but got %x", InodeDefaultSymlinkPermissions, inode.Permissions)
	}

	if inode.Flags&Ext4ExtentsFL != 0 {
		t.Errorf("inode has Ext4ExtentsFL flag set but we were expecting inline data")
	}

	if inode.Flags&Ext4InlineDataFL == 0 {
		t.Errorf("inode has missing Ext4InlineDataFL flag")
	}

	if int64(inode.SizeLower) != size {
		t.Errorf("inode has incorrect size -- expect %d but got %d", size, inode.SizeLower)
	}

	sectors := blocks * SectorsPerBlock
	if int64(inode.Sectors) != sectors {
		t.Errorf("inode has incorrect number of sectors -- expect %d but got %d", sectors, inode.Sectors)
	}

	iblock := inode.Block[:]

	got := string(iblock[:inode.SizeLower])
	if got != link {
		t.Errorf("inode has unexpected inline value -- expect %s but got %v", link, []byte(got))
	}

}

func TestGenerateInodeLargeSymlink(t *testing.T) {

	link := strings.Repeat("v", 256)
	size := int64(len(link))
	blocks := divide(size, BlockSize)

	n := &node{
		node: &vio.TreeNode{
			File: vio.CustomFile(vio.CustomFileArgs{
				Size:               int(size),
				ReadCloser:         ioutil.NopCloser(strings.NewReader(link)),
				IsSymlink:          true,
				IsSymlinkNotCached: false,
				Symlink:            link,
			}),
			NodeSequenceNumber: 42,
			Links:              1,
		},
		start:   2,
		content: uint32(blocks),
		fs:      uint32(blocks),
	}

	inode := generateInode(n, &testContentMapper{})

	if inode.Permissions != InodeDefaultSymlinkPermissions {
		t.Errorf("inode has incorrect file permissions -- expect %x but got %x", InodeDefaultSymlinkPermissions, inode.Permissions)
	}

	if inode.Flags&Ext4ExtentsFL == 0 {
		t.Errorf("inode has missing Ext4ExtentsFL flag")
	}

	if int64(inode.SizeLower) != size {
		t.Errorf("inode has incorrect size -- expect %d but got %d", size, inode.SizeLower)
	}

	sectors := blocks * SectorsPerBlock
	if int64(inode.Sectors) != sectors {
		t.Errorf("inode has incorrect number of sectors -- expect %d but got %d", sectors, inode.Sectors)
	}

	iblock := bytes.NewReader(inode.Block[:])

	hdr := new(ExtentHeader)
	err := binary.Read(iblock, binary.LittleEndian, hdr)
	if err != nil {
		t.Error(err)
	}

	if hdr.Magic != ExtentMagic {
		t.Errorf("inode has missing extents tree")
	}

	if hdr.Entries != 1 {
		t.Errorf("inode extents tree has bad number of entries -- expect %d but got %d", 1, hdr.Entries)
	}

	var extents [4]Extent
	err = binary.Read(iblock, binary.LittleEndian, &extents)
	if err != nil {
		t.Error(err)
	}

	assertExtent := func(i int, block, start, l int64) {

		extent := extents[i]

		if int64(extent.Block) != block {
			t.Errorf("inode extents tree entry %d has unexpected 'Block' value -- expect %d but got %d", i, block, extent.Block)
		}

		if int64(extent.StartLo) != start {
			t.Errorf("inode extents tree entry %d has unexpected 'StartLo' value -- expect %d but got %d", i, start, extent.StartLo)
		}

		if int64(extent.Len) != l {
			t.Errorf("inode extents tree entry %d has unexpected 'Len' value -- expect %d but got %d", i, l, extent.Len)
		}

	}

	assertExtent(0, 0, 7, 1)
	assertExtent(1, 0, 0, 0)
	assertExtent(2, 0, 0, 0)
	assertExtent(3, 0, 0, 0)

}

func TestGenerateInodeFlatDirectory(t *testing.T) {

	blocks := int64(1)

	vnode := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			IsDir: true,
		}),
		NodeSequenceNumber: 42,
		Links:              3,
	}

	cnode := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			IsDir: true,
		}),
		NodeSequenceNumber: 43,
		Links:              2,
		Parent:             vnode,
	}

	vnode.Children = append(vnode.Children, cnode)

	n := &node{
		node:    vnode,
		start:   2,
		content: uint32(blocks),
		fs:      uint32(blocks),
	}

	inode := generateInode(n, &testContentMapper{})

	if inode.Permissions != InodeDefaultDirectoryPermissions {
		t.Errorf("inode has incorrect file permissions -- expect %x but got %x", InodeDefaultDirectoryPermissions, inode.Permissions)
	}

	if inode.Flags&Ext4ExtentsFL == 0 {
		t.Errorf("inode has missing Ext4ExtentsFL flag")
	}

	if inode.Flags&Ext4IndexFL != 0 {
		t.Errorf("inode has unexpected Ext4IndexFL flag")
	}

	if int64(inode.SizeLower) != blocks*BlockSize {
		t.Errorf("inode has incorrect size -- expect %d but got %d", blocks*BlockSize, inode.SizeLower)
	}

	sectors := blocks * SectorsPerBlock
	if int64(inode.Sectors) != sectors {
		t.Errorf("inode has incorrect number of sectors -- expect %d but got %d", sectors, inode.Sectors)
	}

	iblock := bytes.NewReader(inode.Block[:])

	hdr := new(ExtentHeader)
	err := binary.Read(iblock, binary.LittleEndian, hdr)
	if err != nil {
		t.Error(err)
	}

	if hdr.Magic != ExtentMagic {
		t.Errorf("inode has missing extents tree")
	}

	if hdr.Entries != 1 {
		t.Errorf("inode extents tree has bad number of entries -- expect %d but got %d", 1, hdr.Entries)
	}

	var extents [4]Extent
	err = binary.Read(iblock, binary.LittleEndian, &extents)
	if err != nil {
		t.Error(err)
	}

	assertExtent := func(i int, block, start, l int64) {

		extent := extents[i]

		if int64(extent.Block) != block {
			t.Errorf("inode extents tree entry %d has unexpected 'Block' value -- expect %d but got %d", i, block, extent.Block)
		}

		if int64(extent.StartLo) != start {
			t.Errorf("inode extents tree entry %d has unexpected 'StartLo' value -- expect %d but got %d", i, start, extent.StartLo)
		}

		if int64(extent.Len) != l {
			t.Errorf("inode extents tree entry %d has unexpected 'Len' value -- expect %d but got %d", i, l, extent.Len)
		}

	}

	assertExtent(0, 0, 7, 1)
	assertExtent(1, 0, 0, 0)
	assertExtent(2, 0, 0, 0)
	assertExtent(3, 0, 0, 0)

}

func TestGenerateInodeHashDirectory(t *testing.T) {

	blocks := int64(3)

	vnode := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			IsDir: true,
		}),
		NodeSequenceNumber: 42,
		Links:              60,
	}

	n := &node{
		node:    vnode,
		start:   2,
		content: uint32(blocks),
		fs:      uint32(blocks),
	}

	inode := generateInode(n, &testContentMapper{})

	if inode.Permissions != InodeDefaultDirectoryPermissions {
		t.Errorf("inode has incorrect file permissions -- expect %x but got %x", InodeDefaultDirectoryPermissions, inode.Permissions)
	}

	if inode.Flags&Ext4ExtentsFL == 0 {
		t.Errorf("inode has missing Ext4ExtentsFL flag")
	}

	if inode.Flags&Ext4IndexFL == 0 {
		t.Errorf("inode has missing Ext4IndexFL flag")
	}

	if int64(inode.SizeLower) != blocks*BlockSize {
		t.Errorf("inode has incorrect size -- expect %d but got %d", blocks*BlockSize, inode.SizeLower)
	}

	sectors := blocks * SectorsPerBlock
	if int64(inode.Sectors) != sectors {
		t.Errorf("inode has incorrect number of sectors -- expect %d but got %d", sectors, inode.Sectors)
	}

	iblock := bytes.NewReader(inode.Block[:])

	hdr := new(ExtentHeader)
	err := binary.Read(iblock, binary.LittleEndian, hdr)
	if err != nil {
		t.Error(err)
	}

	if hdr.Magic != ExtentMagic {
		t.Errorf("inode has missing extents tree")
	}

	if hdr.Entries != 1 {
		t.Errorf("inode extents tree has bad number of entries -- expect %d but got %d", 1, hdr.Entries)
	}

	var extents [4]Extent
	err = binary.Read(iblock, binary.LittleEndian, &extents)
	if err != nil {
		t.Error(err)
	}

	assertExtent := func(i int, block, start, l int64) {

		extent := extents[i]

		if int64(extent.Block) != block {
			t.Errorf("inode extents tree entry %d has unexpected 'Block' value -- expect %d but got %d", i, block, extent.Block)
		}

		if int64(extent.StartLo) != start {
			t.Errorf("inode extents tree entry %d has unexpected 'StartLo' value -- expect %d but got %d", i, start, extent.StartLo)
		}

		if int64(extent.Len) != l {
			t.Errorf("inode extents tree entry %d has unexpected 'Len' value -- expect %d but got %d", i, l, extent.Len)
		}

	}

	assertExtent(0, 0, 7, 3)
	assertExtent(1, 0, 0, 0)
	assertExtent(2, 0, 0, 0)
	assertExtent(3, 0, 0, 0)

}

// TODO: Inode of a large file (at least 5 extents)
