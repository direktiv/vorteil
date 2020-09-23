package ext

import (
	"context"
	"io"
	"path/filepath"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vio"
)

// CompilerArgs organizes all inputs necessary to create a new Compiler. Because
// the compiler is designed to be configured in stages by the caller very little
// goes here.
type CompilerArgs struct {
	FileTree vio.FileTree
	Logger   elog.Logger
}

// Compiler keeps all variables and settings for a single file-system compile
// operation. Compilation occurs in a number of stages that have to happen in an
// exact sequence: NewCompiler, Commit, Precompile, Compile. The compiler was
// designed this way to allow it to: be adjustable in terms of its contents and
// its capacity, pre-calculate its minimum space requirements, pre-calculate
// where "holes" will exist, and build in one continuous stream.
type Compiler struct {
	log elog.Logger

	minFreeInodes  int64
	minFreeSpace   int64
	minInodes      int64
	minInodesPer64 int64
	minDataBlocks  int64
	minSize        int64
	inodes         int64

	compiler
}

// NewCompiler returns an initialized Compiler object. The next necessary step
// is to call Commit on this Compiler, but before doing so it is possible to
// modify its contents with functions like Mkdir, AddFile, and
// IncreaseMinimumInodes (to name a few).
func NewCompiler(args *CompilerArgs) *Compiler {
	c := new(Compiler)
	c.tree = args.FileTree
	c.log = args.Logger
	return c
}

// Mkdir allows the caller to add an empty directory to the file-system at
// 'path' if no file or directory is already mapped there. This function must
// be called before calling Commit, otherwise the behaviour is undefined.
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

// AddFile allows the caller to add a file to the file-system at 'path',
// resolving any collisions by overwriting them if 'force' is true. This
// function must be called before calling Commit, otherwise the behaviour is
// undefined.
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

// IncreaseMinimumInodes allows the caller to force in some extra empty inodes
// on top of whatever would have otherwise been there. This function can only be
// called before calling Commit, otherwise the behaviour is undefined.
func (c *Compiler) IncreaseMinimumInodes(inodes int64) {
	c.minFreeInodes += inodes
}

// SetMinimumInodes allows the caller to specify the minimum number of inodes
// that should be built onto the file-system. This function can only be called
// before calling Commit, otherwise the behaviour is undefined.
func (c *Compiler) SetMinimumInodes(inodes int64) {
	c.minInodes = inodes
}

// SetMinimumInodesPer64MiB allows the caller to impose some minimum number of
// inodes relative to the total file-system image size. This function can only
// be caleld before calling Commit, otherwise the behaviour is undefined.
func (c *Compiler) SetMinimumInodesPer64MiB(inodes int64) {
	c.minInodesPer64 = inodes
}

// IncreaseMinimumFreeSpace allows the caller to add a minimum amount of extra
// free space to the file-system image in bytes. The Compiler cannot know how
// the space will be used in practice, which means it's possible this free
// space will be consumed by pointer blocks or wasted by small files during
// runtime.
func (c *Compiler) IncreaseMinimumFreeSpace(space int64) {
	c.minFreeSpace += space
}

type nodeBlocks struct {
	node    *vio.TreeNode
	start   int64
	content uint32
	fs      uint32
}

// Commit is the second of the four steps necessary to compile a file-system
// image, and should be called sometime after NewCompiler and before Precompile.
// It is responsible for locking-in some values and calculating the minimum size
// of the file-system. Any calls to functions that change the contents or
// capacity of the file-system must be done before this function is called.
func (c *Compiler) Commit(ctx context.Context) error {

	var err error

	c.filledDataBlocks, err = c.scanInodes(ctx, c.tree)
	if err != nil {
		return err
	}

	minInodes := int64(len(c.inodeBlocks)) - 1
	minInodes += c.minFreeInodes
	if c.minInodes < minInodes {
		c.minInodes = minInodes
	}

	minBlocks := c.filledDataBlocks
	minBlocks += divide(c.minFreeSpace, BlockSize)
	c.minDataBlocks = minBlocks

	c.minSize, err = c.calculateMinimumSize(ctx, c.minDataBlocks, c.minInodes, c.minInodesPer64)
	if err != nil {
		return err
	}

	return nil

}

// MinimumSize returns the minimum number of bytes needed to contain the
// file-system image. It can be called after a successful call to Commit, and is
// intended to provide useful information to the caller that can help it
// determine what final size should be supplied to the Precompile step as an
// argument.
func (c *Compiler) MinimumSize() int64 {
	return c.minSize
}

// Precompile locks in the file-system size and computes the entire structure of
// the final file-system image. It does this so that the RegionIsHole function
// can be used by the caller in situations where identifying empty regions in
// the image is important, like when embedding the file-system into a sparse
// VMDK image file. It must be called only after a successful Commit and is
// necessary before calling the final function: Compile.
func (c *Compiler) Precompile(ctx context.Context, size int64) error {

	err := c.setPrecompileConstants(size, c.minDataBlocks, c.minInodes, c.minInodesPer64)
	if err != nil {
		return err
	}

	c.fillBlockUsageBitmap()

	c.initSuperblock()

	err = c.generateBGDT()
	if err != nil {
		return err
	}

	c.log.Debugf("Total Inodes:  %v", c.inodesPerGroup*c.groups)

	return nil

}

// RegionIsHole can be called after a successful Precompile. Its purpose is to
// provide advance notice to sparse disk image formatting logic on regions
// within the image that will be completely empty. The two args are measured in
// bytes, and the function returns true if every byte starting at begin and
// continuing for the full size is zeroed.
func (c *Compiler) RegionIsHole(begin, size int64) bool {
	return c.regionIsHole(begin, size)
}

// Compile is the final operation performed by the Compiler, and should only be
// called after a successful call to the Precompile function. It writes the
// file-system to the provided io.WriteSeeker, w. Despite using io.Seeker
// functionality to improve performance, the compiler has been written in a way
// such that it never needs to seek "backwards", which means you can wrap any
// io.Writer with a vio.WriteSeeker and it will work.
//
// NOTE: Many seek calls are not relative (they use io.SeekStart for the whence
// argument), and they expect the "start" to be the place where the file-system
// begins. For this reason it is recommended to use a vio.WriteSeeker here when
// building full disk images.
func (c *Compiler) Compile(ctx context.Context, w io.WriteSeeker) error {

	var err error

	for g := int64(0); g < c.groups; g++ {

		err = c.writeBlockGroup(ctx, w, g)
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
