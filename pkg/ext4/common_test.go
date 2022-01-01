package ext4

import (
	"bytes"
	"io"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/vorteil/vorteil/pkg/vio"
)

func TestGrowToBlock(t *testing.T) {

	buf := new(bytes.Buffer)
	growToBlock(buf)

	if buf.Len() != BlockSize {
		t.Errorf("growToBlock failed to grow an empty block")
	}

	growToBlock(buf)
	if buf.Len() != BlockSize {
		t.Errorf("growToBlock shouldn't have grown a block that was already fully formed")
	}

	_, err := io.CopyN(buf, vio.Zeroes, 1)
	if err != nil {
		t.Error(err)
	}

	growToBlock(buf)
	if buf.Len() != 2*BlockSize {
		t.Errorf("growToBlock doesn't grow multiple blocks correctly")
	}

}

func TestCalculateBlocksFromSize(t *testing.T) {

	blocks := calculateBlocksFromSize(0)
	if blocks != 0 {
		t.Errorf("calculateBlocksFromSize doesn't handle size zero correctly")
	}

	blocks = calculateBlocksFromSize(1)
	if blocks != 1 {
		t.Errorf("calculateBlocksFromSize failed on size 1")
	}

	blocks = calculateBlocksFromSize(BlockSize)
	if blocks != 1 {
		t.Errorf("calculateBlocksFromSize failed on size 4096")
	}

	blocks = calculateBlocksFromSize(BlockSize*6 + 1)
	if blocks != 7 {
		t.Errorf("calculateBlocksFromSize failed")
	}

}

func TestCalculateRegularFileBlocks(t *testing.T) {

	size := 0
	f := vio.CustomFile(vio.CustomFileArgs{
		Size:       size,
		ReadCloser: ioutil.NopCloser(io.LimitReader(vio.Zeroes, int64(size))),
	})
	blocks := calculateRegularFileBlocks(f)

	if blocks != 0 {
		t.Errorf("calculateRegularFileBlocks returned a bad number of blocks for an empty file")
	}

	size = 50
	f = vio.CustomFile(vio.CustomFileArgs{
		Size:       size,
		ReadCloser: ioutil.NopCloser(io.LimitReader(vio.Zeroes, int64(size))),
	})
	blocks = calculateRegularFileBlocks(f)

	if blocks != 1 {
		t.Errorf("calculateRegularFileBlocks returned a bad number of blocks for a tiny file (we don't support inline files yet)")
	}

	size = 200
	f = vio.CustomFile(vio.CustomFileArgs{
		Size:       size,
		ReadCloser: ioutil.NopCloser(io.LimitReader(vio.Zeroes, int64(size))),
	})
	blocks = calculateRegularFileBlocks(f)

	if blocks != 1 {
		t.Errorf("calculateRegularFileBlocks returned a bad number of blocks for a small file")
	}

	size = BlockSize*7 + 1
	f = vio.CustomFile(vio.CustomFileArgs{
		Size:       size,
		ReadCloser: ioutil.NopCloser(io.LimitReader(vio.Zeroes, int64(size))),
	})
	blocks = calculateRegularFileBlocks(f)

	if blocks != 8 {
		t.Errorf("calculateRegularFileBlocks returned a bad number of blocks for a bigger file")
	}

}

func TestCalculateSymlinkBlocks(t *testing.T) {

	link := ""
	f := vio.CustomFile(vio.CustomFileArgs{
		IsSymlink: true,
		Symlink:   link,
		Size:      len(link),
	})
	blocks := calculateSymlinkBlocks(f)

	if blocks != 0 {
		t.Errorf("calculateSymlinkBlocks returned a bad number of blocks for an empty symlink")
	}

	link = "vorteil"
	f = vio.CustomFile(vio.CustomFileArgs{
		IsSymlink: true,
		Symlink:   link,
		Size:      len(link),
	})
	blocks = calculateSymlinkBlocks(f)

	if blocks != 0 {
		t.Errorf("calculateSymlinkBlocks returned a bad number of blocks for an inline symlink")
	}

	link = strings.Repeat("v", 60)
	f = vio.CustomFile(vio.CustomFileArgs{
		IsSymlink: true,
		Symlink:   link,
		Size:      len(link),
	})
	blocks = calculateSymlinkBlocks(f)

	if blocks != 1 {
		t.Errorf("calculateSymlinkBlocks returned a bad number of blocks for a large symlink")
	}

}
