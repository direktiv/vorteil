package ext

import "github.com/vorteil/vorteil/pkg/vio"

type blockUsage struct {
	filledDataBlocks       int64
	blocks                 int64
	blocksPerGroup         int64
	overheadBlocksPerGroup int64
	dataBlocksPerGroup     int64
	groups                 int64
	blocksPerBGDT          int64
	blocksPerInodeTable    int64
	unallocatedBlocks      int64
	blockUsageBitmap       []uint64

	inodesPerGroup    int64
	unallocatedInodes int64
	dirsInGroup       []int64
}

func (c *blockUsage) mapDBtoBlockAddr(in int64) int64 {
	g := in / c.dataBlocksPerGroup
	o := in % c.dataBlocksPerGroup
	return g*c.blocksPerGroup + c.overheadBlocksPerGroup + o
}

func (c *blockUsage) fillBlockUsageBitmap() {

	c.blockUsageBitmap = make([]uint64, divide(c.blocks, 64), divide(c.blocks, 64))

	// data is packed in compactly from low addresses to high addresses sequentially
	// calculate first available data block so we can fill the block usage bitmap efficiently
	g := c.filledDataBlocks / c.dataBlocksPerGroup
	o := c.filledDataBlocks % c.dataBlocksPerGroup
	bno := g*c.blocksPerGroup + c.overheadBlocksPerGroup + o

	for i := int64(0); i < bno/64; i++ {
		c.blockUsageBitmap[i] = 0xFFFFFFFFFFFFFFFF
	}

	i := bno / 64
	j := bno % 64
	c.blockUsageBitmap[i] = 0xFFFFFFFFFFFFFFFF >> (64 - j)

	// manually insert overhead bits for subsequent groups
	for x := g + 1; x < c.groups; x++ {
		for y := int64(0); y < c.overheadBlocksPerGroup; y++ {
			bno = x*c.blocksPerGroup + y
			i = bno / 64
			j = bno % 64
			c.blockUsageBitmap[i] |= 1 << j
		}
	}

	if c.blocksPerGroup != BlockSize*8 {
		panic("fix this")
	}

	// mark bits for overhang in the final group
	for bno := c.blocks; bno < int64(len(c.blockUsageBitmap)*64); bno++ {
		i = bno / 64
		j = bno % 64
		c.blockUsageBitmap[i] |= 1 << j
	}

}

func (c *blockUsage) regionIsHole(begin, size int64) bool {

	first := begin / BlockSize
	end := begin + size
	last := (end - 1) / BlockSize

	for bno := first; bno <= last; bno++ {

		i := bno / 64
		j := bno % 64

		if int(i) < len(c.blockUsageBitmap) && (c.blockUsageBitmap[i]&(0x1<<j)) > 0 {
			return false
		}

	}

	return true
}

func (c *blockUsage) countDirsInGroups(tree vio.FileTree) error {

	c.dirsInGroup = make([]int64, c.groups)
	ino := int64(9)

	err := tree.Walk(func(path string, f vio.File) error {
		ino++
		if f.IsDir() {
			g := (ino - 1) / c.inodesPerGroup
			c.dirsInGroup[g]++
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}
