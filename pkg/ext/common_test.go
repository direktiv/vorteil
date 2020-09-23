package ext

import (
	"io"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/vorteil/vorteil/pkg/vio"
)

func TestIndirectBlocksCalculation(t *testing.T) {

	// If there are 12 blocks or fewer no indirect blocks are necessary.
	if calculateNumberOfIndirectBlocks(0) != 0 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 0 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(1) != 0 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(7) != 0 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 7 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(12) != 0 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 12 blocks incorrectly")
	}

	// If there are more than 12 blocks but no more than 12 + refsPerBlock there
	// should be exactly one indirect block.
	if calculateNumberOfIndirectBlocks(13) != 1 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 13 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(128) != 1 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 128 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(1024) != 1 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1024 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(1036) != 1 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1036 blocks incorrectly")
	}

	// If there are more than 12 + refsPerBlock we are looking at multiple
	// indirect blocks.
	if calculateNumberOfIndirectBlocks(1037) != 3 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1037 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(1049612) != 1026 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1049612 blocks incorrectly")
	}

	if calculateNumberOfIndirectBlocks(1049613) != 1029 {
		t.Fatalf("calculateNumberOfIndirectBlocks calculates 1049613 blocks incorrectly")
	}

}

func TestBlockTypeCalculation(t *testing.T) {

	// If there are 12 blocks or fewer no indirect blocks are necessary.
	if blockType(0) != 0 {
		t.Fatalf("blockType calculates block 0 incorrectly")
	}

	if blockType(1) != 0 {
		t.Fatalf("blockType calculates block 1 incorrectly")
	}

	if blockType(7) != 0 {
		t.Fatalf("blockType calculates block 7 incorrectly")
	}

	if blockType(11) != 0 {
		t.Fatalf("blockType calculates block 11 incorrectly")
	}

	// Moving into the first indirect region.
	if blockType(12) != 1 {
		t.Fatalf("blockType calculates block 12 incorrectly")
	}

	if blockType(13) != 0 {
		t.Fatalf("blockType calculates block 13 incorrectly")
	}

	if blockType(128) != 0 {
		t.Fatalf("blockType calculates block 128 incorrectly")
	}

	if blockType(1024) != 0 {
		t.Fatalf("blockType calculates block 1024 incorrectly")
	}

	if blockType(1036) != 0 {
		t.Fatalf("blockType calculates block 1036 incorrectly")
	}

	// Moving into second indirect region
	if blockType(1037) != 2 {
		t.Fatalf("blockType calculates block 1037 incorrectly")
	}

	if blockType(1038) != 1 {
		t.Fatalf("blockType calculates block 1038 incorrectly")
	}

	if blockType(1039) != 0 {
		t.Fatalf("blockType calculates block 1039 incorrectly")
	}

	if blockType(2062) != 0 {
		t.Fatalf("blockType calculates block 2062 incorrectly")
	}

	if blockType(2063) != 1 {
		t.Fatalf("blockType calculates block 2063 incorrectly")
	}

	if blockType(2064) != 0 {
		t.Fatalf("blockType calculates block 2064 incorrectly")
	}

	// Moving into the third indirect region
	if blockType(1050637) != 0 {
		t.Fatalf("blockType calculates block 1050637 incorrectly")
	}

	if blockType(1050638) != 3 {
		t.Fatalf("blockType calculates block 1050638 incorrectly")
	}

	if blockType(1050639) != 2 {
		t.Fatalf("blockType calculates block 1050639 incorrectly")
	}

	if blockType(1050640) != 1 {
		t.Fatalf("blockType calculates block 1050640 incorrectly")
	}

	if blockType(1050641) != 0 {
		t.Fatalf("blockType calculates block 1050641 incorrectly")
	}

	if blockType(1051664) != 0 {
		t.Fatalf("blockType calculates block 1051664 incorrectly")
	}

	if blockType(1051665) != 1 {
		t.Fatalf("blockType calculates block 1051665 incorrectly")
	}

	if blockType(1051666) != 0 {
		t.Fatalf("blockType calculates block 1051666 incorrectly")
	}

	if blockType(2100239) != 0 {
		t.Fatalf("blockType calculates block 2100239 incorrectly")
	}

	if blockType(2100240) != 2 {
		t.Fatalf("blockType calculates block 2100240 incorrectly")
	}

	if blockType(2100241) != 1 {
		t.Fatalf("blockType calculates block 2100241 incorrectly")
	}

	if blockType(2100242) != 0 {
		t.Fatalf("blockType calculates block 2100242 incorrectly")
	}

}

func TestSymlinkSizeCalculation(t *testing.T) {

	var name string
	var f vio.File
	var content, fs int64

	name = "Vorteil"
	f = vio.CustomFile(vio.CustomFileArgs{
		Size:       len(name),
		IsSymlink:  true,
		ReadCloser: ioutil.NopCloser(strings.NewReader(name)),
	})
	defer f.Close()

	content, fs = calculateSymlinkBlocks(f)
	if content != 1 || fs != 1 {
		t.Fatalf("calculateSymlinkSize calculates small symlink sizes incorrectly")
	}

	name = ""
	f = vio.CustomFile(vio.CustomFileArgs{
		Size:       len(name),
		IsSymlink:  true,
		ReadCloser: ioutil.NopCloser(strings.NewReader(name)),
	})
	defer f.Close()

	content, fs = calculateSymlinkBlocks(f)
	if content != 0 || fs != 0 {
		t.Fatalf("calculateSymlinkSize calculates zero-length symlink sizes incorrectly")
	}

}

func TestFileSizeCalculation(t *testing.T) {

	var f vio.File
	var size int64
	var content, fs int64

	size = 0
	f = vio.CustomFile(vio.CustomFileArgs{
		Size:       int(size),
		ReadCloser: ioutil.NopCloser(io.LimitReader(vio.Zeroes, size)),
	})
	defer f.Close()

	content, fs = calculateRegularFileBlocks(f)
	if content != 0 || fs != 0 {
		t.Fatalf("calculateRegularFileSize calculates zero-length file sizes incorrectly")
	}

	size = 1234
	f = vio.CustomFile(vio.CustomFileArgs{
		Size:       int(size),
		ReadCloser: ioutil.NopCloser(io.LimitReader(vio.Zeroes, size)),
	})
	defer f.Close()

	content, fs = calculateRegularFileBlocks(f)
	if content != 1 || fs != 1 {
		t.Fatalf("calculateRegularFileSize calculates tiny file sizes incorrectly")
	}

	size = 20480
	f = vio.CustomFile(vio.CustomFileArgs{
		Size:       int(size),
		ReadCloser: ioutil.NopCloser(io.LimitReader(vio.Zeroes, size)),
	})
	defer f.Close()

	content, fs = calculateRegularFileBlocks(f)
	if content != 5 || fs != 5 {
		t.Fatalf("calculateRegularFileSize calculates small file sizes incorrectly")
	}

	size = 53248
	f = vio.CustomFile(vio.CustomFileArgs{
		Size:       int(size),
		ReadCloser: ioutil.NopCloser(io.LimitReader(vio.Zeroes, size)),
	})
	defer f.Close()

	content, fs = calculateRegularFileBlocks(f)
	if content != 13 || fs != 14 {
		t.Fatalf("calculateRegularFileSize calculates medium file sizes incorrectly")
	}

}

func TestDirectorySizeCalculation(t *testing.T) {

	var content, fs int64
	var n *vio.TreeNode

	n = &vio.TreeNode{
		Parent:   n,
		Children: []*vio.TreeNode{},
	}

	content, fs = calculateDirectoryBlocks(n)
	if content != 1 || fs != 1 {
		t.Fatalf("calculateDirectorySize calculates empty directory sizes incorrectly")
	}

	// n.Children = append(n.Children, &vio.TreeNode{
	// 	Parent: n,
	// 	File: ,
	// })

}
