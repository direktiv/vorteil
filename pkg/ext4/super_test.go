package ext4

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"github.com/vorteil/vorteil/pkg/vio"
)

func TestSuperblockStruct(t *testing.T) {

	superblock := &Superblock{}

	// check that a couple of the fields are at the correct offsets
	var offset int

	offset = offsetOf(superblock, &superblock.DefaultMountOpts)
	if offset != 0x100 {
		t.Errorf("struct Superblock has been corrupted (a field is offset incorrectly)")
	}

	offset = offsetOf(superblock, &superblock.MountOptions)
	if offset != 0x200 {
		t.Errorf("struct Superblock has been corrupted (a field is offset incorrectly)")
	}

}

func TestBlockGroupDescriptorStruct(t *testing.T) {

	// check that the struct is the correct size
	bgd := &BlockGroupDescriptor{}
	size := binary.Size(bgd)

	if size != DescriptorSize {
		t.Errorf("struct BlockGroupDescriptor is the wrong size -- expect %d but got %d", DescriptorSize, size)
	}

	// check that a couple of the fields are at the correct offsets
	var offset int

	offset = offsetOf(bgd, &bgd.UnusedInodes)
	if offset != 28 {
		t.Errorf("struct BlockGroupDescriptor has been corrupted (a field is offset incorrectly)")
	}

}

func TestSuperLayout(t *testing.T) {

	blocks := int64(BlocksPerGroup*12 - 100)
	nodes := make([]node, 20)
	nodes[RootDirInode].fs = 1
	nodes[RootDirInode].node = &vio.TreeNode{
		NodeSequenceNumber: RootDirInode,
		File: vio.CustomFile(vio.CustomFileArgs{
			IsDir: true,
		}),
	}
	nodes[RootDirInode].node.Parent = nodes[RootDirInode].node

	super := new(super)
	super.init(blocks, 100, &nodes)

	if super.totalBlocks != blocks {
		t.Errorf("layout planned poorly -- incorrect number of blocks")
	}

	if super.totalGroups() != 12 {
		t.Errorf("layout planned poorly -- incorrect number of groups")
	}

	if super.groupsPerFlex() != 4 {
		t.Errorf("layout planned poorly -- incorrect number of groups per flex")
	}

	if super.totalFlexes() != 3 {
		t.Errorf("layout planned poorly -- incorrect number of flexes")
	}

	for g := int64(1); g < super.totalGroups(); g++ {
		if super.descriptors[g].directories > 0 {
			t.Errorf("layout planned poorly -- incorrect number of directories in group %d", g)
		}
		if super.descriptors[g].freeInodes != 100 {
			t.Errorf("layout planned poorly -- incorrect number of free inodes in group %d", g)
		}
		expectedFreeBlocks := int64(BlocksPerGroup)
		if g+1 == super.totalGroups() {
			expectedFreeBlocks = super.totalBlocks - g*BlocksPerGroup
		}
		if g%super.groupsPerFlex() == 0 {
			expectedFreeBlocks -= 24
		}
		if int64(super.descriptors[g].freeBlocks) != expectedFreeBlocks {
			t.Errorf("layout planned poorly -- incorrect number of free blocks in group %d %d %d", g, expectedFreeBlocks, int64(super.descriptors[g].freeBlocks))
		}
	}

	buf := new(bytes.Buffer)
	w, err := vio.WriteSeeker(buf)
	if err != nil {
		t.Error(err)
	}

	// NOTE: checking the validity of the following output would be too tedious. This is just running to detect panics.
	err = super.writeSuperblockAndBGDT(context.Background(), w, 0)
	if err != nil {
		t.Error(err)
	}

	err = super.writeFlexGroupMetaData(context.Background(), w, 0)
	if err != nil {
		t.Error(err)
	}

}
