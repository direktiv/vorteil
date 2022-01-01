package xfs

import (
	"bytes"
	"io"
	"io/ioutil"
	"testing"

	"github.com/vorteil/vorteil/pkg/vio"
)

func init() {
	overrideInodeTranslator = func(x int64) uint64 {
		return uint64(x)
	}
}

func TestEmptyShortformDirectoryData(t *testing.T) {

	c := new(compiler)

	parent := byte(42)

	n := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			Name:  "dir",
			IsDir: true,
		}),
		Children:           make([]*vio.TreeNode, 0),
		NodeSequenceNumber: int64(parent) + 1,
		Links:              2,
	}

	p := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			Name:  "parent",
			IsDir: true,
		}),
		Children:           []*vio.TreeNode{n},
		NodeSequenceNumber: int64(parent),
		Links:              2,
	}

	n.Parent = p

	expect := []byte{
		0, 0, 0, 0, 0, parent,
	}

	// empty directory
	got := generateShortFormDirectoryData(c, n)

	if !bytes.Equal(expect, got) {
		t.Errorf("expected %v, got %v", expect, got)
	}
}

func TestBasicShortformDirectoryData(t *testing.T) {

	c := new(compiler)

	ino := byte(42)

	c0 := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			Name: "apple",
		}),
		NodeSequenceNumber: int64(ino + 1),
		Links:              1,
	}

	c1 := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			Name: "pear",
		}),
		NodeSequenceNumber: int64(ino + 2),
		Links:              1,
	}

	c2 := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			Name: "cantaloupe",
		}),
		NodeSequenceNumber: int64(ino + 3),
		Links:              1,
	}

	c3 := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			Name: "orange",
		}),
		NodeSequenceNumber: int64(ino + 4),
		Links:              1,
	}

	n := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			Name:  "dir",
			IsDir: true,
		}),
		Children:           []*vio.TreeNode{c0, c1, c2, c3},
		NodeSequenceNumber: int64(ino),
		Links:              2,
	}

	expect := []byte{
		4, 0, 0, 0, 0, ino,
		5, 0, 0x30, 'a', 'p', 'p', 'l', 'e', 0, 0, 0, ino + 1,
		4, 0, 0x48, 'p', 'e', 'a', 'r', 0, 0, 0, ino + 2,
		10, 0, 0x58, 'c', 'a', 'n', 't', 'a', 'l', 'o', 'u', 'p', 'e', 0, 0, 0, ino + 3,
		6, 0, 0x70, 'o', 'r', 'a', 'n', 'g', 'e', 0, 0, 0, ino + 4,
	}

	// empty directory
	got := generateShortFormDirectoryData(c, n)

	if !bytes.Equal(expect, got) {
		t.Errorf("expected %v, got %v", expect, got)
	}
}

// TODO: short-form directory data that overflows
// TODO: short-form directory data using high address range inodes (64 bit)

func TestHashName(t *testing.T) {

	expect := uint32(0)
	got := hashname("")

	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	expect = 781758355
	got = hashname("vorteil")
	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	expect = 736419341
	got = hashname("vorteil++")
	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	expect = 4067321834
	got = hashname("Vorteil.io")

	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}
}

func TestZeroes(t *testing.T) {
	_, err := io.CopyN(ioutil.Discard, vio.Zeroes, 0x1000)
	if err != nil {
		t.Error(err)
	}

	_, err = vio.Zeroes.Read([]byte{})
	if err != nil {
		t.Error(err)
	}
}

func TestPrecompilerSize(t *testing.T) {
	p := new(precompiler)
	p.totalBlocks = 1024
	p.exponents.blockSize = 12
	expect := int64(1024 * (1 << 12))
	got := p.MinimumSize()
	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}
}
func TestCompilerSize(t *testing.T) {
	c := new(compiler)
	c.totalBlocks = 1024
	c.exponents.blockSize = 12
	expect := int64(1024 * (1 << 12))
	got := c.Size()
	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}
}

func TestSetBlockSize(t *testing.T) {
	p := new(precompiler)
	p.exponents.sectorSize = 9
	p.pageSize = 4096

	err := p.setBlockSize(0)
	if err != nil {
		t.Error(err)
	}

	expect := int64(4096)
	got := p.blockSize()
	if expect != got {
		t.Errorf("expected default block size to be %v, got %v", expect, got)
	}

	err = p.setBlockSize(2048)
	if err != nil {
		t.Error(err)
	}

	err = p.setBlockSize(4096)
	if err != nil {
		t.Error(err)
	}

	err = p.setBlockSize(p.sectorSize() - 1)
	if err == nil {
		t.Errorf("expected to get a failure for allowing block size smaller than sector size")
	}

	err = p.setBlockSize(p.pageSize + 1)
	if err == nil {
		t.Errorf("expected to get a failure for allowing block size larger than page size")
	}

	p.exponents.blockSize = 0
	err = p.setBlockSize(p.sectorSize() * 3)
	if err == nil {
		t.Errorf("expected to get a failure for allowing block size that isn't a power of two")
	}
}

func TestInodeNumber(t *testing.T) {
	c := &constants{}
	c.exponents.blocksPerAllocGroup = 12
	c.exponents.blockSize = 12
	c.exponents.inodeSize = 9

	expect := uint64(42)
	got := c.inodeNumber(0, 42)

	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	expect = 42<<15 | 42
	got = c.inodeNumber(42, 42)

	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	c.exponents.blocksPerAllocGroup = 16
	c.exponents.inodeSize = 8

	expect = uint64(42)
	got = c.inodeNumber(0, 42)

	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	expect = 42<<20 | 42
	got = c.inodeNumber(42, 42)

	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}
}

func TestBlockNumber(t *testing.T) {
	c := &constants{}
	c.exponents.blocksPerAllocGroup = 12

	expect := uint64(42)
	got := c.blockNumber(0, 42)

	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	expect = 42<<12 | 42
	got = c.blockNumber(42, 42)

	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	c.exponents.blocksPerAllocGroup = 16

	expect = uint64(42)
	got = c.blockNumber(0, 42)

	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	expect = 42<<16 | 42
	got = c.blockNumber(42, 42)

	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}
}

func TestExponents(t *testing.T) {

	c := &constants{}
	c.exponents.inodeSize = 9
	c.exponents.blockSize = 12

	expect := int64(1) << 9
	got := c.inodeSize()
	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	expect = 1 << 12
	got = c.blockSize()
	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	expect = 1 << 3
	got = c.inodesPerBlock()
	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	c.exponents.inodeSize = 11
	c.exponents.blockSize = 11

	expect = 1 << 11
	got = c.inodeSize()
	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	expect = 1 << 11
	got = c.blockSize()
	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

	expect = 1
	got = c.inodesPerBlock()
	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

}

func TestConstants(t *testing.T) {

	c := &constants{}
	c.exponents.blocksPerAllocGroup = 12

	expect := int64(1) << 12
	got := c.blocksPerAllocGroup()
	if expect != got {
		t.Errorf("expected %v, got %v", expect, got)
	}

}
