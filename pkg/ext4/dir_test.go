package ext4

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/vorteil/vorteil/pkg/vio"
)

func TestHashDirectoryRootStruct(t *testing.T) {

	// check that the struct is the correct size
	root := &HashDirectoryRoot{}
	size := binary.Size(root)

	if size != BlockSize {
		t.Errorf("struct HashDirectoryRoot is the wrong size -- expect %d but got %d", BlockSize, size)
	}

	// check that a couple of the fields are at the correct offsets
	var offset int

	offset = offsetOf(root, &root.Limit)
	if offset != 32 {
		t.Errorf("struct HashDirectoryRoot has been corrupted (a field is offset incorrectly)")
	}

}

func TestDentryHash(t *testing.T) {

	// These are checks against known constants.

	if dentryHash("") != 0x67452300 {
		t.Errorf("the tiny encryption algorithm has been broken")
	}

	if dentryHash(".") != 0x31FD669C {
		t.Errorf("the tiny encryption algorithm has been broken")
	}

	if dentryHash("..") != 0xBC44B5BE {
		t.Errorf("the tiny encryption algorithm has been broken")
	}

	if dentryHash("vorteil") != 0x1D76D232 {
		t.Errorf("the tiny encryption algorithm has been broken")
	}

	if dentryHash(strings.Repeat("v", 48)) != 0x25FC974A {
		t.Errorf("the tiny encryption algorithm has been broken")
	}

}

func TestDentry(t *testing.T) {

	testDentry := func(name string, min, actual int) {
		expect := min
		if dentryMinLength(name) != int64(expect) {
			t.Errorf("dentry length calculation is broken for '%s' -- expect %d but got %d", name, expect, dentryMinLength(name))
		}

		expect = actual
		buf := new(bytes.Buffer)
		err := writeDentry(buf, name, &dentry{
			Inode:    2,
			RecLen:   uint16(expect),
			NameLen:  1,
			FileType: FTypeDir,
		})
		if err != nil {
			t.Error(err)
		}

		if buf.Len() != expect {
			t.Errorf("dentry writer produced an unexpected dentry length -- expect %d but got %d", expect, buf.Len())
		}
	}

	testDentry(".", 12, 12)
	testDentry("vorteil", 16, 16)
	testDentry(".vorteil", 20, 1024)

}

func TestLinearDir(t *testing.T) {

	p := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			IsDir: true,
		}),
		NodeSequenceNumber: 2,
		Links:              3,
	}
	p.Parent = p

	p.Children = append(p.Children, &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			IsDir: true,
			Name:  "test_dir",
		}),
		NodeSequenceNumber: 11,
		Links:              2,
		Parent:             p,
	})

	p.Children = append(p.Children, &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			Name: "test_file",
		}),
		NodeSequenceNumber: 12,
		Links:              1,
		Parent:             p,
	})

	p.Children = append(p.Children, &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			Name:      "test_symlink",
			IsSymlink: true,
		}),
		NodeSequenceNumber: 13,
		Links:              1,
		Parent:             p,
	})

	calculatedSize := calculateLinearDirectorySize(p)

	n := &node{
		node:    p,
		start:   0,
		content: 1,
		fs:      1,
	}

	data := generateLinearDirectoryData(n)
	if int64(len(data)) != calculatedSize {
		t.Errorf("calculated directory size doesn't match generated data")
	}

	expect := []byte{
		2, 0, 0, 0, 12, 0, 1, 2, '.', 0, 0, 0,
		2, 0, 0, 0, 12, 0, 2, 2, '.', '.', 0, 0,
		11, 0, 0, 0, 20, 0, 8, 2, 't', 'e', 's', 't', '_', 'd', 'i', 'r', 0, 0, 0, 0,
		12, 0, 0, 0, 20, 0, 9, 1, 't', 'e', 's', 't', '_', 'f', 'i', 'l', 'e', 0, 0, 0,
		13, 0, 0, 0, 0xC0, 0x0F, 12, 7, 't', 'e', 's', 't', '_', 's', 'y', 'm', 'l', 'i', 'n', 'k', 0, 0, 0, 0,
	}
	if !bytes.HasPrefix(data, expect) {
		t.Errorf("generated linear directory isn't like we expected --\n\texpect %v\n\t   got %v", expect, data)
	}

}

func TestLargeLinearDir(t *testing.T) {

	p := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			IsDir: true,
		}),
		NodeSequenceNumber: 2,
		Links:              3,
	}
	p.Parent = p

	var counter int64
	counter = 11

	attach := func(name string) {
		p.Children = append(p.Children, &vio.TreeNode{
			File: vio.CustomFile(vio.CustomFileArgs{
				Name: name,
			}),
			NodeSequenceNumber: counter,
			Links:              1,
			Parent:             p,
		})
		counter++
	}

	for i := 0; i < 100; i++ {
		attach(strings.Repeat("v", i))
	}

	calculatedSize := calculateLinearDirectorySize(p)

	n := &node{
		node:    p,
		start:   0,
		content: uint32(divide(calculatedSize, BlockSize)),
		fs:      uint32(divide(calculatedSize, BlockSize)),
	}

	if n.fs < 2 {
		t.Fatalf("bad test doesn't have a big enough directory")
	}

	data := generateLinearDirectoryData(n)
	if int64(len(data)) != calculatedSize {
		t.Errorf("calculated directory size doesn't match generated data")
	}

	// NOTE: testing for coverage only, since the lines that are tested by this are only used when the linear directory exceeds one block, which can't currently happen since we swap to a hash directory when this occurs.

}

func TestHashDir(t *testing.T) {

	p := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			IsDir: true,
		}),
		NodeSequenceNumber: 2,
		Links:              3,
	}
	p.Parent = p

	var counter int64
	counter = 11

	attach := func(name string) {
		p.Children = append(p.Children, &vio.TreeNode{
			File: vio.CustomFile(vio.CustomFileArgs{
				Name: name,
			}),
			NodeSequenceNumber: counter,
			Links:              1,
			Parent:             p,
		})
		counter++
	}

	for i := 0; i < 100; i++ {
		attach(strings.Repeat("v", i))
	}

	calculatedSize := calculateHashDirectorySize(p)

	n := &node{
		node:    p,
		start:   0,
		content: uint32(divide(calculatedSize, BlockSize)),
		fs:      uint32(divide(calculatedSize, BlockSize)),
	}

	if n.fs < 2 {
		t.Fatalf("bad test doesn't have a big enough directory")
	}

	data := generateHashDirectoryData(n)
	if int64(len(data)) != calculatedSize {
		t.Errorf("calculated directory size doesn't match generated data")
	}

}

// TODO: HUGE hash dir (one that would require a non-flat tree)
