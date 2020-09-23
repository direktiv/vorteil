package ext

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"path"
	"path/filepath"
	"time"

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

	tree           vio.FileTree
	minFreeInodes  int64
	minFreeSpace   int64
	minInodes      int64
	minInodesPer64 int64
	minDataBlocks  int64
	minSize        int64
	inodes         int64
	size           int64

	filledDataBlocks       int64
	blocks                 int64
	blocksPerGroup         int64
	blocksPerBGDT          int64
	blocksPerInodeTable    int64
	overheadBlocksPerGroup int64
	dataBlocksPerGroup     int64
	groups                 int64
	inodesPerGroup         int64
	unallocatedBlocks      int64
	unallocatedInodes      int64
	blockUsageBitmap       []uint64
	inodeBlocks            []nodeBlocks
	superblock             Superblock
	bgdt                   []byte
	dirsInGroup            []int64

	activeNode       int64
	activeNodeReader io.Reader
	activeNodeBlock  int64
	activeNodeBlocks int64
	activeNodeStart  int64
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

func (c *Compiler) scanInodes(ctx context.Context) error {

	var err error
	var ino, minInodes, contentDelta, fsDelta int64
	ino = 9
	minInodes = 9 + int64(c.tree.NodeCount())
	c.inodeBlocks = make([]nodeBlocks, minInodes+1, minInodes+1) // +1 because inodes start counting at 1 rather than 0, and I don't want to correct for that in every array index.

	err = c.tree.WalkNode(func(path string, n *vio.TreeNode) error {

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

		c.inodeBlocks[ino].start = c.filledDataBlocks
		c.inodeBlocks[ino].node = n
		c.inodeBlocks[ino].content = uint32(contentDelta)
		c.inodeBlocks[ino].fs = uint32(fsDelta)
		n.NodeSequenceNumber = ino
		c.filledDataBlocks += fsDelta

		return nil

	})
	if err != nil {
		return err
	}

	c.inodeBlocks[2] = c.inodeBlocks[10]
	c.inodeBlocks[10] = nodeBlocks{}
	c.inodeBlocks[2].node.NodeSequenceNumber = 2

	return nil

}

// Commit is the second of the four steps necessary to compile a file-system
// image, and should be called sometime after NewCompiler and before Precompile.
// It is responsible for locking-in some values and calculating the minimum size
// of the file-system. Any calls to functions that change the contents or
// capacity of the file-system must be done before this function is called.
func (c *Compiler) Commit(ctx context.Context) error {

	err := c.scanInodes(ctx)
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

	err = c.calculateMinimalStructure(ctx)
	if err != nil {
		return err
	}

	return nil

}

func (c *Compiler) calculateMinimalStructure(ctx context.Context) error {

	var err error
	var blocks, groups, blocksPerGroup, inodesPerGroup, blocksPerBGDT int64
	var blocksPerInodeTable, overheadBlocksPerGroup, dataBlocksPerGroup int64
	var groupsNeededToContainData, totalBlocks int64
	blocks = c.minDataBlocks
	blocksPerGroup = BlockSize * 8 // 8 bits per byte in the bitmap
	groups = divide(blocks, blocksPerGroup)

	for {
		if err = ctx.Err(); err != nil {
			return err
		}

		inodesPerGroup = divide(c.minInodes, groups)

		// each block group is 128 MiB, so we double the per64 value if it's set
		if inodesPerGroup < c.minInodesPer64*2 {
			inodesPerGroup = c.minInodesPer64 * 2
		}

		inodesPerGroup = align(inodesPerGroup, InodesPerBlock)
		blocksPerBGDT = divide(groups*BlockGroupDescriptorSize, BlockSize)
		blocksPerInodeTable = inodesPerGroup / InodesPerBlock
		overheadBlocksPerGroup = blocksPerSuperblock + blocksPerBGDT + blocksPerBlockBitmap + blocksPerInodeBitmap + blocksPerInodeTable
		dataBlocksPerGroup = blocksPerGroup - overheadBlocksPerGroup

		groupsNeededToContainData = divide(c.minDataBlocks, dataBlocksPerGroup)
		if groupsNeededToContainData > groups {
			groups = groupsNeededToContainData
			continue
		}

		totalBlocks = (groups - 1) * blocksPerGroup
		totalBlocks += overheadBlocksPerGroup
		if c.minDataBlocks > (groups-1)*dataBlocksPerGroup {
			totalBlocks += c.minDataBlocks % dataBlocksPerGroup
		}
		c.minSize = totalBlocks * BlockSize
		return nil
	}

}

// MinimumSize returns the minimum number of bytes needed to contain the
// file-system image. It can be called after a successful call to Commit, and is
// intended to provide useful information to the caller that can help it
// determine what final size should be supplied to the Precompile step as an
// argument.
func (c *Compiler) MinimumSize() int64 {
	return c.minSize
}

func (c *Compiler) setPrecompileConstants(size int64) error {

	c.size = size
	c.blocks = c.size / BlockSize // this is intentionally not rounded up
	c.blockUsageBitmap = make([]uint64, divide(c.blocks, 64), divide(c.blocks, 64))
	c.blocksPerGroup = BlockSize * 8 // 8 bits per byte in the bitmap
	c.groups = divide(c.blocks, c.blocksPerGroup)
	c.inodesPerGroup = divide(c.minInodes, c.groups)

	if c.inodesPerGroup < c.minInodesPer64*2 {
		c.inodesPerGroup = c.minInodesPer64 * 2
	}

	c.inodesPerGroup = align(c.inodesPerGroup, InodesPerBlock)

	if c.inodesPerGroup > BlockSize*8 {
		return errors.New("minimum inodes required exceeds maximum number of inodes possible at this disk size")
	}

	c.blocksPerBGDT = divide(c.groups*BlockGroupDescriptorSize, BlockSize)
	c.blocksPerInodeTable = c.inodesPerGroup / InodesPerBlock
	c.overheadBlocksPerGroup = blocksPerSuperblock + c.blocksPerBGDT + blocksPerBlockBitmap + blocksPerInodeBitmap + c.blocksPerInodeTable
	c.dataBlocksPerGroup = c.blocksPerGroup - c.overheadBlocksPerGroup
	c.unallocatedBlocks = c.blocks - c.filledDataBlocks - c.groups*c.overheadBlocksPerGroup
	c.unallocatedInodes = c.groups*c.inodesPerGroup - int64(len(c.inodeBlocks)-1)

	c.inodeBlocks[2].node.Parent = c.inodeBlocks[2].node

	groupsNeededToContainData := divide(c.minDataBlocks, c.dataBlocksPerGroup)
	if groupsNeededToContainData > c.groups {
		return errors.New("insufficient size to satisfy minimum data capacity requirements")
	}

	return nil

}

func (c *Compiler) fillBlockUsageBitmap() {

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

func (c *Compiler) initSuperblock() {
	now := time.Now()
	c.superblock.LastMountTime = uint32(now.Unix())
	c.superblock.LastWrittenTime = uint32(now.Unix())
	c.superblock.MountsCheckInterval = 20
	c.superblock.Signature = 0xEF53
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
}

func (c *Compiler) countDirsInGroups() error {

	c.dirsInGroup = make([]int64, c.groups, c.groups)
	ino := int64(9)

	err := c.tree.Walk(func(path string, f vio.File) error {
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

func (c *Compiler) generateBGDT() error {

	err := c.countDirsInGroups()
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

		binary.Write(buf, binary.LittleEndian, uint32(i*c.blocksPerGroup+blocksPerSuperblock+c.blocksPerBGDT))                                           // block usage bitmap
		binary.Write(buf, binary.LittleEndian, uint32(i*c.blocksPerGroup+blocksPerSuperblock+c.blocksPerBGDT+blocksPerBlockBitmap))                      // inode usage bitmap
		binary.Write(buf, binary.LittleEndian, uint32(i*c.blocksPerGroup+blocksPerSuperblock+c.blocksPerBGDT+blocksPerBlockBitmap+blocksPerInodeBitmap)) // inode table
		binary.Write(buf, binary.LittleEndian, uint16(blocks))                                                                                           // unallocated blocks
		binary.Write(buf, binary.LittleEndian, uint16(inodes))
		binary.Write(buf, binary.LittleEndian, uint16(c.dirsInGroup[i])) // directories
		buf.Write(bytes.Repeat([]byte{0}, 14))
	}

	c.bgdt = buf.Bytes()

	return nil
}

// Precompile locks in the file-system size and computes the entire structure of
// the final file-system image. It does this so that the RegionIsHole function
// can be used by the caller in situations where identifying empty regions in
// the image is important, like when embedding the file-system into a sparse
// VMDK image file. It must be called only after a successful Commit and is
// necessary before calling the final function: Compile.
func (c *Compiler) Precompile(ctx context.Context, size int64) error {

	err := c.setPrecompileConstants(size)
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

func (c *Compiler) writeSuperblock(w io.WriteSeeker, g int64) error {

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

func (c *Compiler) writeBGDT(w io.WriteSeeker, g int64) error {

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

func (c *Compiler) writeBlockBitmap(w io.WriteSeeker, g int64) error {

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

func (c *Compiler) writeInodeBitmap(w io.WriteSeeker, g int64) error {

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

func (c *Compiler) mapDBtoBlockAddr(in int64) int64 {
	g := in / c.dataBlocksPerGroup
	o := in % c.dataBlocksPerGroup
	return g*c.blocksPerGroup + c.overheadBlocksPerGroup + o
}

func (c *Compiler) setInodePointers(ino int64, inode *Inode) {

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

func (c *Compiler) writeInode(ino int64, w io.Writer) error {

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

func (c *Compiler) writeInodeTable(w io.WriteSeeker, g int64) error {

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

type dirTuple struct {
	name  string
	inode uint32
}

func (c *Compiler) generateDirectoryData(node *nodeBlocks) (io.Reader, error) {

	var tuples []*dirTuple
	tuples = append(tuples, &dirTuple{name: ".", inode: uint32(node.node.NodeSequenceNumber)})
	tuples = append(tuples, &dirTuple{name: "..", inode: uint32(node.node.Parent.NodeSequenceNumber)})

	for _, child := range node.node.Children {
		tuples = append(tuples, &dirTuple{name: path.Base(child.File.Name()), inode: uint32(child.NodeSequenceNumber)})
	}

	buf := new(bytes.Buffer)
	length := int64(0)
	leftover := int64(BlockSize)

	for i, child := range tuples {
		l := 8 + align(int64(len(child.name)+1), dentryNameAlignment)

		if leftover >= l && (leftover-l == 0 || leftover-l > 8) {
			length += l
			leftover -= l
		} else {
			// add a null entry into the leftover space
			_ = binary.Write(buf, binary.LittleEndian, uint32(0))        // inode
			_ = binary.Write(buf, binary.LittleEndian, uint16(leftover)) // entry size
			_ = binary.Write(buf, binary.LittleEndian, uint16(0))        // name length
			_, _ = buf.Write(bytes.Repeat([]byte{0}, int(leftover-8)))   // padding

			length += leftover
			length += l
			leftover = int64(BlockSize) - l
		}

		if leftover < 8 || i == len(tuples)-1 {
			l += leftover
			length += leftover
			leftover = int64(BlockSize)
		}

		_ = binary.Write(buf, binary.LittleEndian, child.inode)                      // inode
		_ = binary.Write(buf, binary.LittleEndian, uint16(l))                        // entry size
		_ = binary.Write(buf, binary.LittleEndian, uint16(len(child.name)))          // name length
		_ = binary.Write(buf, binary.LittleEndian, append([]byte(child.name), 0))    // name
		_, _ = buf.Write(bytes.Repeat([]byte{0}, int(l-8-int64(len(child.name))-1))) // padding

	}

	buf.Grow(int(leftover) % int(BlockSize))

	return bytes.NewReader(buf.Bytes()), nil

}

func (c *Compiler) getNextNode() *nodeBlocks {

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

func (c *Compiler) prepNextDataBlock() error {

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
			c.activeNodeReader, err = c.generateDirectoryData(node)
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

func (c *Compiler) writeBlock(w io.Writer) error {

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
			_ = binary.Write(buffer, binary.LittleEndian, uint32(c.mapDBtoBlockAddr(j+c.activeNodeStart)))
		}
	case 2: // it is a double indirect pointer block
		for j = c.activeNodeBlock + 1; j < c.activeNodeBlocks && j < c.activeNodeBlock+1+(1+refsPerBlock)*refsPerBlock; j = j + (1 + refsPerBlock) {
			_ = binary.Write(buffer, binary.LittleEndian, uint32(c.mapDBtoBlockAddr(j+c.activeNodeStart)))
		}
	case 3: // it is a triple indirect pointer block
		for j = c.activeNodeBlock + 1; j < c.activeNodeBlocks && j < c.activeNodeBlock+1+(1+refsPerBlock)*refsPerBlock+refsPerBlock*refsPerBlock*refsPerBlock; j = j + (1+refsPerBlock)*refsPerBlock + 1 {
			_ = binary.Write(buffer, binary.LittleEndian, uint32(c.mapDBtoBlockAddr(j+c.activeNodeStart)))
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

func (c *Compiler) nextDataBlock(w io.Writer) error {

	err := c.prepNextDataBlock()
	if err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}

	err = c.writeBlock(w)
	if err != nil {
		return err
	}

	return nil

}

func (c *Compiler) writeDataBlocks(ctx context.Context, w io.WriteSeeker, g int64) error {

	first := g*c.blocksPerGroup + c.overheadBlocksPerGroup
	last := (g+1)*c.blocksPerGroup - 1
	if last >= c.blocks {
		last = c.blocks - 1
	}

	for block := first; block <= last; block++ {

		err := ctx.Err()
		if err != nil {
			return err
		}

		_, err = w.Seek(block*BlockSize, io.SeekStart)
		if err != nil {
			return err
		}

		err = c.nextDataBlock(w)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}

	return nil
}

func (c *Compiler) writeBlockGroupBitmaps(w io.WriteSeeker, g int64) error {

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

func (c *Compiler) writeBlockGroupMetadata(w io.WriteSeeker, g int64) error {

	err := c.writeSuperblock(w, g)
	if err != nil {
		return err
	}

	err = c.writeBGDT(w, g)
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

func (c *Compiler) writeBlockGroup(ctx context.Context, w io.WriteSeeker, g int64) error {

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
