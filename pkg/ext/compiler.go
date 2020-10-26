package ext

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"time"

	"github.com/vorteil/vorteil/pkg/vio"
)

type compiler struct {
	nodeTracker
	blockUsage

	tree vio.FileTree
	size int64

	superblock Superblock
	bgdt       []byte
}

func (c *compiler) calculateMinimumSize(ctx context.Context, minDataBlocks, minInodes, minInodesPer64 int64) (int64, error) {

	var err error
	var blocks, groups, blocksPerGroup, inodesPerGroup, blocksPerBGDT int64
	var blocksPerInodeTable, overheadBlocksPerGroup, dataBlocksPerGroup int64
	var groupsNeededToContainData, totalBlocks int64
	blocks = minDataBlocks
	blocksPerGroup = BlockSize * 8 // 8 bits per byte in the bitmap
	groups = divide(blocks, blocksPerGroup)

	for {
		if err = ctx.Err(); err != nil {
			return 0, err
		}

		inodesPerGroup = divide(minInodes, groups)

		// each block group is 128 MiB, so we double the per64 value if it's set
		if inodesPerGroup < minInodesPer64*2 {
			inodesPerGroup = minInodesPer64 * 2
		}

		inodesPerGroup = align(inodesPerGroup, InodesPerBlock)
		blocksPerBGDT = divide(groups*BlockGroupDescriptorSize, BlockSize)
		blocksPerInodeTable = inodesPerGroup / InodesPerBlock
		overheadBlocksPerGroup = blocksPerSuperblock + blocksPerBGDT + blocksPerBlockBitmap + blocksPerInodeBitmap + blocksPerInodeTable
		dataBlocksPerGroup = blocksPerGroup - overheadBlocksPerGroup

		groupsNeededToContainData = divide(minDataBlocks, dataBlocksPerGroup)
		if groupsNeededToContainData > groups {
			groups = groupsNeededToContainData
			continue
		}

		totalBlocks = (groups - 1) * blocksPerGroup
		totalBlocks += overheadBlocksPerGroup
		if minDataBlocks > (groups-1)*dataBlocksPerGroup {
			totalBlocks += minDataBlocks % dataBlocksPerGroup
		}
		minSize := totalBlocks * BlockSize
		return minSize, nil
	}

}

func (c *compiler) setPrecompileConstants(size, minDataBlocks, minInodes, minInodesPer64 int64) error {

	c.size = size
	c.blocks = c.size / BlockSize    // this is intentionally not rounded up
	c.blocksPerGroup = BlockSize * 8 // 8 bits per byte in the bitmap
	c.groups = divide(c.blocks, c.blocksPerGroup)

retry:
	c.inodesPerGroup = divide(minInodes, c.groups)

	if c.inodesPerGroup < minInodesPer64*2 {
		c.inodesPerGroup = minInodesPer64 * 2
	}

	c.inodesPerGroup = align(c.inodesPerGroup, InodesPerBlock)

	if c.inodesPerGroup > BlockSize*8 {
		return errors.New("minimum inodes required exceeds maximum number of inodes possible at this disk size")
	}

	c.blocksPerBGDT = divide(c.groups*BlockGroupDescriptorSize, BlockSize)
	c.blocksPerInodeTable = c.inodesPerGroup / InodesPerBlock
	c.overheadBlocksPerGroup = blocksPerSuperblock + c.blocksPerBGDT + blocksPerBlockBitmap + blocksPerInodeBitmap + c.blocksPerInodeTable

	// check for an edge case where the final block groups is too small to contain its metadata
	if x := c.blocks % c.blocksPerGroup; x > 0 && x < c.overheadBlocksPerGroup {
		c.groups--
		c.blocks = c.groups * c.blocksPerGroup
		goto retry
	}

	c.dataBlocksPerGroup = c.blocksPerGroup - c.overheadBlocksPerGroup
	c.unallocatedBlocks = c.blocks - c.filledDataBlocks - c.groups*c.overheadBlocksPerGroup
	c.unallocatedInodes = c.groups*c.inodesPerGroup - int64(len(c.inodeBlocks)-1)

	c.inodeBlocks[RootDirInode].node.Parent = c.inodeBlocks[RootDirInode].node

	groupsNeededToContainData := divide(minDataBlocks, c.dataBlocksPerGroup)
	if groupsNeededToContainData > c.groups {
		return errors.New("insufficient size to satisfy minimum data capacity requirements")
	}

	return nil

}

func (c *compiler) initSuperblock() {
	now := time.Now()
	c.superblock.LastMountTime = uint32(now.Unix())
	c.superblock.LastWrittenTime = uint32(now.Unix())
	c.superblock.MountsCheckInterval = 20
	c.superblock.Signature = Signature
	c.superblock.State = 1
	c.superblock.TimeLastCheck = uint32(now.Unix())
	c.superblock.SuperUser = SuperUID
	c.superblock.SuperGroup = SuperGID
	c.superblock.BlockSize = 2
	c.superblock.FragmentSize = 2
	c.superblock.TotalBlocks = uint32(c.blocks)
	c.superblock.TotalInodes = uint32(c.inodesPerGroup * c.groups)
	c.superblock.BlocksPerGroup = uint32(c.blocksPerGroup)
	c.superblock.InodesPerGroup = uint32(c.inodesPerGroup)
	c.superblock.FragmentsPerGroup = uint32(c.blocksPerGroup)
	c.superblock.UnallocatedBlocks = uint32(c.unallocatedBlocks)
	c.superblock.UnallocatedInodes = uint32(c.unallocatedInodes)
	c.superblock.RequiredFeatures = IncompatFiletype
}

func (c *compiler) generateBGDT() error {

	err := c.countDirsInGroups(c.tree)
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	for i := int64(0); i < c.groups; i++ {
		blocks := int64(0)
		dpb := c.blocksPerGroup - c.overheadBlocksPerGroup
		if dpb*i > c.filledDataBlocks {
			blocks = dpb
		} else if dpb*(i+1) > c.filledDataBlocks {
			blocks = dpb - c.filledDataBlocks%dpb
		}
		if i == c.groups-1 {
			dif := c.groups*c.blocksPerGroup - c.blocks
			blocks -= dif
		}
		inodes := int64(0)
		ipb := c.inodesPerGroup
		claimedInodes := int64(len(c.inodeBlocks) - 1)
		if ipb*i+1 > claimedInodes {
			inodes = ipb
		} else if ipb*(i+1)+1 > claimedInodes {
			inodes = ipb - claimedInodes%ipb
		}

		bgdte := &BlockGroupDescriptorTableEntry{
			BlockBitmapBlockAddr: uint32(i*c.blocksPerGroup + blocksPerSuperblock + c.blocksPerBGDT),
			InodeBitmapBlockAddr: uint32(i*c.blocksPerGroup + blocksPerSuperblock + c.blocksPerBGDT + blocksPerBlockBitmap),
			InodeTableBlockAddr:  uint32(i*c.blocksPerGroup + blocksPerSuperblock + c.blocksPerBGDT + blocksPerBlockBitmap + blocksPerInodeBitmap),
			UnallocatedBlocks:    uint16(blocks),
			UnallocatedInodes:    uint16(inodes),
			Directories:          uint16(c.dirsInGroup[i]),
		}

		_ = binary.Write(buf, binary.LittleEndian, bgdte)
	}

	c.bgdt = buf.Bytes()

	return nil
}

func (c *compiler) writeSuperblock(w io.WriteSeeker, g int64) error {

	_, err := w.Seek(g*c.blocksPerGroup*BlockSize, io.SeekStart)
	if err != nil {
		return err
	}

	if g == 0 {
		_, err = io.CopyN(w, vio.Zeroes, 1024)
		if err != nil {
			return err
		}
	}

	c.superblock.SuperblockNumber = uint32(g * c.blocksPerGroup)
	err = binary.Write(w, binary.LittleEndian, &c.superblock)
	if err != nil {
		return err
	}

	return nil

}

func (c *compiler) writeBGDT(w io.WriteSeeker, g int64) error {

	_, err := w.Seek((g*c.blocksPerGroup+blocksPerSuperblock)*BlockSize, io.SeekStart)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, bytes.NewReader(c.bgdt))
	if err != nil {
		return err
	}

	return nil
}

func (c *compiler) writeBlockBitmap(w io.WriteSeeker, g int64) error {

	_, err := w.Seek((g*c.blocksPerGroup+blocksPerSuperblock+c.blocksPerBGDT)*BlockSize, io.SeekStart)
	if err != nil {
		return err
	}

	if c.blocksPerGroup%64 != 0 {
		panic("fix this")
	}

	first := (c.blocksPerGroup * g) / 64
	l := c.blocksPerGroup / 64
	slice := c.blockUsageBitmap[first:]
	if int64(len(slice)) > l {
		slice = slice[:l]
	}

	err = binary.Write(w, binary.LittleEndian, slice)
	if err != nil {
		return err
	}

	l = BlockSize - (int64(len(slice)) * 8)
	for i := int64(0); i < l; i++ {
		err = binary.Write(w, binary.LittleEndian, uint8(0xFF))
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *compiler) writeInodeBitmap(w io.WriteSeeker, g int64) error {

	_, err := w.Seek((g*c.blocksPerGroup+blocksPerSuperblock+c.blocksPerBGDT+blocksPerBlockBitmap)*BlockSize, io.SeekStart)
	if err != nil {
		return err
	}

	bitmap := bytes.Repeat([]byte{0xFF}, BlockSize)

	x := int64(len(c.inodeBlocks)-1) - (g * c.inodesPerGroup)
	if x < 0 {
		x = 0
	} else if x > c.inodesPerGroup {
		x = c.inodesPerGroup
	}
	free := c.inodesPerGroup - x
	for i := c.inodesPerGroup - free; i < c.inodesPerGroup; i++ {
		x := i / 8
		y := i % 8
		bitmap[x] &^= 0x1 << y
	}

	err = binary.Write(w, binary.LittleEndian, bitmap)
	if err != nil {
		return err
	}

	return nil
}

func (c *compiler) setInodePointers(ino int64, inode *Inode) {

	node := c.inodeBlocks[ino]
	start := int64(node.start)
	length := int64(node.fs)

	// direct pointers
	for i := int64(0); i < maxDirectPointers && i < length; i++ {
		inode.DirectPointer[i] = uint32(c.mapDBtoBlockAddr(start + i))
	}

	// singly indirect
	if length > maxDirectPointers {
		inode.SinglyIndirect = uint32(c.mapDBtoBlockAddr(start + maxDirectPointers))
	} else {
		return
	}

	// doubly indirect
	refsPerBlock := int64(BlockSize / 4)
	if length > refsPerBlock+maxDirectPointers+1 {
		inode.DoublyIndirect = uint32(c.mapDBtoBlockAddr(start + refsPerBlock + maxDirectPointers + 1))
	} else {
		return
	}

	// triply indirect
	if length > 12+1+refsPerBlock+1+refsPerBlock+refsPerBlock*refsPerBlock {
		inode.TriplyIndirect = uint32(c.mapDBtoBlockAddr(start + maxDirectPointers + 1 + refsPerBlock + 1 + refsPerBlock + refsPerBlock*refsPerBlock))
	}

}

func (c *compiler) writeInode(ino int64, w io.Writer) error {

	inode := &Inode{}

	if int64(len(c.inodeBlocks)) <= ino || c.inodeBlocks[ino].node == nil {

		err := binary.Write(w, binary.LittleEndian, inode)
		if err != nil {
			return err
		}

		return nil
	}

	node := c.inodeBlocks[ino]
	if node.node.File.IsDir() {
		inode.SizeLower = uint32(node.content * BlockSize)
		inode.Permissions = inodeDirectoryPermissions
	} else if node.node.File.IsSymlink() {
		inode.SizeLower = uint32(node.node.File.Size())
		inode.Permissions = inodeSymlinkPermissions
	} else {
		inode.SizeLower = uint32(node.node.File.Size())
		inode.Permissions = inodeRegularFilePermissions
	}

	inode.Links = 1
	if node.node.File.IsDir() {
		inode.Links++
		for _, child := range node.node.Children {
			if child.File.IsDir() {
				inode.Links++
			}
		}
	}

	inode.UID = SuperUID
	inode.GID = SuperGID
	inode.Sectors = node.fs * (BlockSize / SectorSize)
	c.setInodePointers(ino, inode)

	err := binary.Write(w, binary.LittleEndian, inode)
	if err != nil {
		return err
	}

	return nil
}

func (c *compiler) writeInodeTable(w io.WriteSeeker, g int64) error {

	_, err := w.Seek((g*c.blocksPerGroup+blocksPerSuperblock+c.blocksPerBGDT+blocksPerBlockBitmap+blocksPerInodeBitmap)*BlockSize, io.SeekStart)
	if err != nil {
		return err
	}

	for i := int64(1); i <= c.inodesPerGroup; i++ {
		ino := i + g*c.inodesPerGroup
		err = c.writeInode(ino, w)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *compiler) writeDataBlocks(ctx context.Context, w io.WriteSeeker, g int64) error {

	first := g*c.blocksPerGroup + c.overheadBlocksPerGroup
	last := (g+1)*c.blocksPerGroup - 1
	if last >= c.blocks {
		last = c.blocks - 1
	}

	_, err := w.Seek(first*BlockSize, io.SeekStart)
	if err != nil {
		return err
	}

	for block := first; block <= last; block++ {

		err := ctx.Err()
		if err != nil {
			return err
		}

		err = c.writeNextDataBlock(w, c.mapDBtoBlockAddr)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	return nil
}

func (c *compiler) writeSuperblockAndBGDT(w io.WriteSeeker, g int64) error {

	err := c.writeSuperblock(w, g)
	if err != nil {
		return err
	}

	err = c.writeBGDT(w, g)
	if err != nil {
		return err
	}

	return nil

}

func (c *compiler) writeBlockGroupBitmaps(w io.WriteSeeker, g int64) error {

	err := c.writeBlockBitmap(w, g)
	if err != nil {
		return err
	}

	err = c.writeInodeBitmap(w, g)
	if err != nil {
		return err
	}

	return nil

}

func (c *compiler) writeBlockGroupMetadata(w io.WriteSeeker, g int64) error {

	err := c.writeSuperblockAndBGDT(w, g)
	if err != nil {
		return err
	}

	err = c.writeBlockGroupBitmaps(w, g)
	if err != nil {
		return err
	}

	err = c.writeInodeTable(w, g)
	if err != nil {
		return err
	}

	return nil

}

func (c *compiler) writeBlockGroup(ctx context.Context, w io.WriteSeeker, g int64) error {

	err := c.writeBlockGroupMetadata(w, g)
	if err != nil {
		return err
	}

	err = c.writeDataBlocks(ctx, w, g)
	if err != nil {
		return err
	}

	return nil

}
