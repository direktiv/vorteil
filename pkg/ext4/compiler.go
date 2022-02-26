package ext4

import (
	"context"
	"io"
	"path/filepath"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vio"
)

type CompilerArgs struct {
	FileTree vio.FileTree
	Logger   elog.Logger
}

type Compiler struct {
	log  elog.Logger
	tree vio.FileTree

	planner
	super
	data
	size int64
}

func NewCompiler(args *CompilerArgs) *Compiler {
	return &Compiler{
		tree: args.FileTree,
		log:  args.Logger,
	}
}

func (c *Compiler) Mkdir(path string) error {

	_, base := filepath.Split(path)
	err := c.tree.Map(path, vio.CustomFile(vio.CustomFileArgs{
		Name:  base,
		IsDir: true,
	}))
	if err != nil {
		return err
	}

	return nil

}

func (c *Compiler) AddFile(path string, r io.ReadCloser, size int64, force bool) error {

	_, base := filepath.Split(path)
	err := c.tree.Map(path, vio.CustomFile(vio.CustomFileArgs{
		Name:       base,
		Size:       int(size),
		ReadCloser: r,
	}))
	if err != nil {
		return err
	}

	return nil

}

func (c *Compiler) IncreaseMinimumInodes(inodes int64) {
	c.minFreeInodes += inodes
}

func (c *Compiler) SetMinimumInodes(inodes int64) {
	c.minInodes = inodes
}

func (c *Compiler) SetMinimumInodesPer64MiB(inodes int64) {
	c.minInodesPer64 = inodes
}

func (c *Compiler) IncreaseMinimumFreeSpace(space int64) {
	c.minFreeSpace += space
}

func (c *Compiler) Commit(ctx context.Context) error {

	return c.planner.commit(ctx, c.tree)

}

func (c *Compiler) MinimumSize() int64 {
	return c.minSize
}

func (c *Compiler) Precompile(ctx context.Context, size int64) error {

	err := c.setPrecompileConstants(size, c.filledDataBlocks, c.minInodes, c.minInodesPer64)
	if err != nil {
		return err
	}

	return nil

}

func (c *Compiler) RegionIsHole(begin, size int64) bool {
	return c.super.regionIsHole(begin, size)
}

func (c *Compiler) Compile(ctx context.Context, w io.WriteSeeker) error {

	var err error

	err = c.writeSuperblockAndBGDT(ctx, w, 0)
	if err != nil {
		return err
	}

	for f := int64(0); f < c.totalFlexes(); f++ {

		offset := BlockSize * BlocksPerGroup * c.groupsPerFlex() * f
		if f == 0 {
			offset += BlockSize * c.superOverheadBlocks()
		}

		_, err = w.Seek(offset, io.SeekStart)
		if err != nil {
			return err
		}

		err = c.writeFlexGroupMetaData(ctx, w, f)
		if err != nil {
			return err
		}

		offset += BlockSize * c.flexOverheadBlocks()

		_, err = w.Seek(offset, io.SeekStart)
		if err != nil {
			return err
		}

		b := c.groupsPerFlex() * BlocksPerGroup * (f + 1)
		if c.totalBlocks < b {
			b = c.totalBlocks
		}
		b -= offset / BlockSize

		err = c.writeDataBlocks(ctx, w, b, &c.super)
		if err != nil {
			return err
		}

	}

	// seek to the end of the image
	_, err = w.Seek(c.size, io.SeekStart)
	if err != nil {
		return err
	}

	return nil

}
