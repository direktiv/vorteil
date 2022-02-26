package ext4

import (
	"bytes"
	"context"
	"io"
	"sort"
)

type offsetOrderedNodes []*node

func (x offsetOrderedNodes) Len() int {
	return len(x)
}

func (x offsetOrderedNodes) Less(i, j int) bool {
	return x[i].start < x[j].start
}

func (x offsetOrderedNodes) Swap(i, j int) {
	tmp := x[i]
	x[i] = x[j]
	x[j] = tmp
}

type data struct {
	nodes  offsetOrderedNodes
	idx    int64
	reader io.Reader
	block  int64
	blocks int64
}

func (d *data) offsetOrderInodeBlocks(inodes *[]node) {
	d.nodes = make(offsetOrderedNodes, len(*inodes))
	for i := range *inodes {
		d.nodes[i] = &(*inodes)[i]
	}
	sort.Sort(d.nodes)
	d.idx = -1
}

func (d *data) init(inodes *[]node) {

	d.offsetOrderInodeBlocks(inodes)

}

func (d *data) getNextNode() *node {

	for {

		if int64(len(d.nodes)) > d.idx && d.idx >= 0 {
			old := d.nodes[d.idx]
			if old.node != nil {
				_ = old.node.File.Close()
			}
		}

		d.idx++

		if int64(len(d.nodes)) <= d.idx {
			return nil
		}

		if d.nodes[d.idx].fs == 0 {
			continue
		}

		return d.nodes[d.idx]

	}

}

func (d *data) prepNextDataBlock(mapper contentMapper) error {

retry:

	if d.block == d.blocks {

		node := d.getNextNode()
		if node == nil {
			return io.EOF
		}

		d.block = 0
		d.blocks = int64(node.fs)

		// generate dir data into args.objData
		if node.node.File.IsDir() {

			var err error
			d.reader, err = generateDirectoryData(node)
			if err != nil {
				return err
			}

		} else {
			d.reader = node.node.File
		}

		if d.blocks == 0 {
			goto retry
		}

		// prepend an extent tree if necessary
		if node.fs > node.content {
			if node.fs-node.content > 1 {
				panic("only handling 'small' extent trees")
			}

			d.reader = io.MultiReader(bytes.NewReader(extentsBlock(node, mapper)), d.reader)
		}

	}

	return nil

}

func (d *data) writeBlock(w io.Writer) error {

	// write next block
	buffer := new(bytes.Buffer)
	_, err := io.CopyN(buffer, d.reader, BlockSize)
	if err != nil && err != io.EOF {
		return err
	}

	growToBlock(buffer)

	// pad and write
	_, err = io.Copy(w, bytes.NewReader(buffer.Bytes()))
	if err != nil {
		return err
	}

	d.block++

	return nil

}

func (d *data) writeNextDataBlock(w io.Writer, mapper contentMapper) error {

	err := d.prepNextDataBlock(mapper)
	if err != nil {
		return err
	}

	err = d.writeBlock(w)
	if err != nil {
		return err
	}

	return nil

}

func (d *data) writeDataBlocks(ctx context.Context, w io.Writer, n int64, mapper contentMapper) error {

	// TODO: support contents large enough to require extra metadata blocks

	for i := int64(0); i < n; i++ {

		err := ctx.Err()
		if err != nil {
			return err
		}

		err = d.writeNextDataBlock(w, mapper)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

	}

	return nil

}
