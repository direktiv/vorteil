package ext

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"

	"github.com/vorteil/vorteil/pkg/vio"
)

type nodeTracker struct {
	inodeBlocks      []nodeBlocks
	activeNode       int64
	activeNodeReader io.Reader
	activeNodeBlock  int64
	activeNodeBlocks int64
	activeNodeStart  int64
}

func (c *nodeTracker) scanInodes(ctx context.Context, tree vio.FileTree) (int64, error) {

	var err error
	var ino, minInodes, contentDelta, fsDelta, filledDataBlocks int64
	ino = 9
	minInodes = 9 + int64(tree.NodeCount())
	c.inodeBlocks = make([]nodeBlocks, minInodes+1, minInodes+1) // +1 because inodes start counting at 1 rather than 0, and I don't want to correct for that in every array index.

	err = tree.WalkNode(func(path string, n *vio.TreeNode) error {

		if err = ctx.Err(); err != nil {
			return err
		}

		ino++

		if n.File.IsSymlink() {
			contentDelta, fsDelta = calculateSymlinkBlocks(n.File)
		} else if n.File.IsDir() {
			contentDelta, fsDelta = calculateDirectoryBlocks(n)
		} else {
			contentDelta, fsDelta = calculateRegularFileBlocks(n.File)
		}

		c.inodeBlocks[ino].start = filledDataBlocks
		c.inodeBlocks[ino].node = n
		c.inodeBlocks[ino].content = uint32(contentDelta)
		c.inodeBlocks[ino].fs = uint32(fsDelta)
		n.NodeSequenceNumber = ino
		filledDataBlocks += fsDelta

		return nil

	})
	if err != nil {
		return 0, err
	}

	c.inodeBlocks[2] = c.inodeBlocks[10]
	c.inodeBlocks[10] = nodeBlocks{}
	c.inodeBlocks[2].node.NodeSequenceNumber = 2

	return filledDataBlocks, nil

}

func (c *nodeTracker) getNextNode() *nodeBlocks {

	for {

		if int64(len(c.inodeBlocks)) > c.activeNode {
			old := c.inodeBlocks[c.activeNode]
			if old.node != nil {
				_ = old.node.File.Close()
			}
		}

		c.activeNode++
		if int64(len(c.inodeBlocks)) <= c.activeNode {
			return nil // should this panic?
		}

		if c.inodeBlocks[c.activeNode].fs == 0 {
			continue
		}

		return &c.inodeBlocks[c.activeNode]

	}

}

func (c *nodeTracker) prepNextDataBlock() error {

	if c.activeNodeBlock == c.activeNodeBlocks {

		node := c.getNextNode()
		if node == nil {
			return io.EOF
		}

		c.activeNodeBlock = 0
		c.activeNodeBlocks = int64(node.fs)
		c.activeNodeStart = int64(node.start)

		// generate dir data into args.objData
		if node.node.File.IsDir() {

			var err error
			c.activeNodeReader, err = generateDirectoryData(node)
			if err != nil {
				return err
			}

		} else {
			c.activeNodeReader = node.node.File
		}

		if c.activeNodeBlocks == 0 {
			return c.prepNextDataBlock()
		}

	}

	return nil

}

func (c *nodeTracker) writeBlock(w io.Writer, mapDBtoBlockAddr func(int64) int64) error {

	// write next block
	buffer := new(bytes.Buffer)
	btype := blockType(c.activeNodeBlock)
	refsPerBlock := int64(BlockSize / pointerSize)

	var j int64

	switch btype {
	case 0: // it is a data block
		_, err := io.CopyN(buffer, c.activeNodeReader, BlockSize)
		if err != nil && err != io.EOF {
			return err
		}
	case 1: // it is a single indirect pointer block
		for j = c.activeNodeBlock + 1; j < c.activeNodeBlocks && j < refsPerBlock+c.activeNodeBlock+1; j++ {
			_ = binary.Write(buffer, binary.LittleEndian, uint32(mapDBtoBlockAddr(j+c.activeNodeStart)))
		}
	case 2: // it is a double indirect pointer block
		for j = c.activeNodeBlock + 1; j < c.activeNodeBlocks && j < c.activeNodeBlock+1+(1+refsPerBlock)*refsPerBlock; j = j + (1 + refsPerBlock) {
			_ = binary.Write(buffer, binary.LittleEndian, uint32(mapDBtoBlockAddr(j+c.activeNodeStart)))
		}
	case 3: // it is a triple indirect pointer block
		for j = c.activeNodeBlock + 1; j < c.activeNodeBlocks && j < c.activeNodeBlock+1+(1+refsPerBlock)*refsPerBlock+refsPerBlock*refsPerBlock*refsPerBlock; j = j + (1+refsPerBlock)*refsPerBlock + 1 {
			_ = binary.Write(buffer, binary.LittleEndian, uint32(mapDBtoBlockAddr(j+c.activeNodeStart)))
		}
	}

	// pad and write
	_, err := w.Write(buffer.Bytes())
	if err != nil {
		return err
	}

	c.activeNodeBlock++

	return nil

}

func (c *nodeTracker) writeNextDataBlock(w io.Writer, mapDBtoBlockAddr func(int64) int64) error {

	err := c.prepNextDataBlock()
	if err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}

	err = c.writeBlock(w, mapDBtoBlockAddr)
	if err != nil {
		return err
	}

	return nil

}
