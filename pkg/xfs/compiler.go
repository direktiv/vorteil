package xfs

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
	log                                      elog.Logger
	tree                                     vio.FileTree
	minFreeInodes, minInodes, minInodesPer64 int64
	minFreeSpace                             int64

	actualSize  int64
	precompiler *precompiler
	compiler    *compiler
}

func NewCompiler(args *CompilerArgs) *Compiler {
	return &Compiler{
		log:  args.Logger,
		tree: args.FileTree,
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

	args := &precompilerArgs{
		Logger:   c.log,
		Contents: c.tree,
	}
	// TODO args.Options.MinimumFreeInodes NOTE: XFS can create new inodes as needed, so these args aren't usually necessary
	args.Options.MinimumFreeSpace = c.minFreeSpace

	p, err := newBuild(ctx, args)
	if err != nil {
		return err
	}

	c.precompiler = p
	return nil

}

func (c *Compiler) MinimumSize() int64 {
	return c.precompiler.MinimumSize()
}

func (c *Compiler) Precompile(ctx context.Context, size int64) error {

	c.actualSize = size
	bsize := c.precompiler.blockSize()
	size = (size / bsize) * bsize

	compiler, err := c.precompiler.Precompile(ctx, size)
	if err != nil {
		return err
	}

	c.compiler = compiler
	return nil

}

func (c *Compiler) RegionIsHole(begin, size int64) bool {
	// TODO: implement this properly
	return false
}

func (c *Compiler) Compile(ctx context.Context, w io.WriteSeeker) error {

	err := c.compiler.Compile(ctx, w)
	if err != nil {
		return err
	}

	_, err = w.Seek(c.actualSize, io.SeekStart)
	if err != nil {
		return err
	}

	return nil

}
