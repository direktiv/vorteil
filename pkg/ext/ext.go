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

	"github.com/vorteil/vorteil/pkg/vio"
)

const (
	SectorSize               = 512
	BlockSize                = 0x1000
	InodeSize                = 128
	InodesPerBlock           = BlockSize / InodeSize
	BlockGroupDescriptorSize = 32
	blocksPerSuperblock      = 1
	blocksPerBlockBitmap     = 1
	blocksPerInodeBitmap     = 1

	pointerSize         = 4
	maxDirectPointers   = 12
	dentryNameAlignment = 4

	SuperUID                    = 1000
	SuperGID                    = 1000
	inodeDirectoryPermissions   = 0x4000 | 0700
	inodeRegularFilePermissions = 0x8000 | 0700
	inodeSymlinkPermissions     = 0xA000 | 0700
)

type Superblock struct {
	TotalInodes         uint32
	TotalBlocks         uint32
	ReservedBlocks      uint32
	UnallocatedBlocks   uint32
	UnallocatedInodes   uint32
	SuperblockNumber    uint32
	BlockSize           uint32
	FragmentSize        uint32
	BlocksPerGroup      uint32
	FragmentsPerGroup   uint32
	InodesPerGroup      uint32
	LastMountTime       uint32
	LastWrittenTime     uint32
	MountsSinceCheck    uint16
	MountsCheckInterval uint16
	Signature           uint16
	State               uint16
	ErrorProtocol       uint16
	VersionMinor        uint16
	TimeLastCheck       uint32
	TimeCheckInterval   uint32
	OS                  uint32
	VersionMajor        uint32
	SuperUser           uint16
	SuperGroup          uint16
}

type Inode struct {
	Permissions      uint16
	UID              uint16
	SizeLower        uint32
	LastAccessTime   uint32
	CreationTime     uint32
	ModificationTime uint32
	DeletionTime     uint32
	GID              uint16
	Links            uint16
	Sectors          uint32
	Flags            uint32
	OSV              uint32
	DirectPointer    [12]uint32
	SinglyIndirect   uint32
	DoublyIndirect   uint32
	TriplyIndirect   uint32
	GenNo            uint32
	Reserved         [2]uint32
	FragAddr         uint32
	OSStuff          [12]byte
}

func divide(a, b int64) int64 {
	return (a + b - 1) / b
}

func align(a, b int64) int64 {
	return divide(a, b) * b
}

type CompilerArgs struct {
	FileTree vio.FileTree
}

type Compiler struct {
	tree          vio.FileTree
	minFreeInodes int64
	minFreeSpace  int64
	minInodes     int64
	minDataBlocks int64
	minSize       int64
	inodes        int64
	size          int64

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

func NewCompiler(args *CompilerArgs) *Compiler {
	c := new(Compiler)
	c.tree = args.FileTree
	return c
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

func (c *Compiler) IncreaseMinimumFreeSpace(space int64) {
	c.minFreeSpace += space
}

type nodeBlocks struct {
	node    *vio.TreeNode
	start   int64
	content uint32
	fs      uint32
}

func (c *Compiler) calculateNumberofIndirectBlocks(blocks int64) (int64, error) {
	var single, double, triple, x int64
	x = BlockSize / pointerSize
	single = maxDirectPointers
	double = single + x
	triple = double + x*x
	bounds := triple + x*x*x

	switch {
	case blocks <= single:
		return 0, nil
	case blocks <= double:
		return 1, nil
	case blocks <= triple:
		return 1 + 1 + divide(blocks-double, x), nil
	case blocks <= bounds:
		return 1 + 1 + x + 1 + divide(divide(blocks-triple, x), x) + divide(blocks-triple, x), nil
	default:
		return 0, errors.New("file too large for ext2")
	}

}

func (c *Compiler) calculateSymlinkSize(f vio.File) (int64, int64, error) {
	l := int64(f.Size())
	content := divide(l, BlockSize)
	fs, err := c.calculateNumberofIndirectBlocks(content)
	if err != nil {
		return 0, 0, err
	}
	fs += content
	return content, fs, nil
}

func (c *Compiler) calculateRegularFileSize(f vio.File) (int64, int64, error) {
	l := int64(f.Size())
	content := divide(l, BlockSize)
	fs, err := c.calculateNumberofIndirectBlocks(content)
	if err != nil {
		return 0, 0, err
	}
	fs += content
	return content, fs, nil
}

func (c *Compiler) calculateDirectorySize(n *vio.TreeNode) (int64, int64, error) {

	var length, leftover int64

	// '.' entry + ".." entry
	length = 24
	leftover = BlockSize - length
	for i, child := range n.Children {
		name := path.Base(child.File.Name())
		l := int64(8 + (len(name)+1+dentryNameAlignment-1)/dentryNameAlignment)
		if leftover >= l {
			length += l
			leftover -= l
		} else {
			length += leftover
			length += l
			leftover = BlockSize - l
		}
		if leftover < 8 || i == len(n.Children)-1 {
			length += leftover
			leftover = BlockSize
		}
	}

	content := divide(length, BlockSize)
	fs, err := c.calculateNumberofIndirectBlocks(content)
	if err != nil {
		return 0, 0, err
	}
	fs += content
	return content, fs, nil

}

func (c *Compiler) Commit(ctx context.Context) error {

	// minimum size & inode calculations happen here

	var err error
	var ino, minInodes, minBlocks, contentDelta, fsDelta int64
	ino = 9
	minInodes = 9 + int64(c.tree.NodeCount())
	c.inodeBlocks = make([]nodeBlocks, minInodes+1, minInodes+1)

	err = c.tree.WalkNode(func(path string, n *vio.TreeNode) error {

		if err = ctx.Err(); err != nil {
			return err
		}

		ino++

		if n.File.IsSymlink() {
			contentDelta, fsDelta, err = c.calculateSymlinkSize(n.File)
			if err != nil {
				return err
			}
		} else if n.File.IsDir() {
			contentDelta, fsDelta, err = c.calculateDirectorySize(n)
			if err != nil {
				return err
			}
		} else {
			contentDelta, fsDelta, err = c.calculateRegularFileSize(n.File)
			if err != nil {
				return err
			}
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

	minInodes += c.minFreeInodes
	if c.minInodes < minInodes {
		c.minInodes = minInodes
	}

	minBlocks = c.filledDataBlocks
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

func (c *Compiler) MinimumSize() int64 {
	return c.minSize
}

func (c *Compiler) Precompile(ctx context.Context, size int64) error {
	c.size = size
	c.blocks = c.size / BlockSize // this is intentionally not rounded up
	c.blockUsageBitmap = make([]uint64, divide(c.blocks, 64), divide(c.blocks, 64))
	c.blocksPerGroup = BlockSize * 8 // 8 bits per byte in the bitmap
	c.groups = divide(c.blocks, c.blocksPerGroup)
	c.inodesPerGroup = divide(c.minInodes, c.groups)
	c.inodesPerGroup = align(c.inodesPerGroup, InodesPerBlock)
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

	// initialize superblock
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

	// generate bgdt
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
	c.generateBGDT()

	return nil

}

func (c *Compiler) generateBGDT() {
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
}

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

func (c *Compiler) writeSuperblock(ctx context.Context, w io.WriteSeeker, g int64) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	_, err = w.Seek(g*c.blocksPerGroup*BlockSize, io.SeekStart)
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

func (c *Compiler) writeBGDT(ctx context.Context, w io.WriteSeeker, g int64) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	_, err = w.Seek((g*c.blocksPerGroup+blocksPerSuperblock)*BlockSize, io.SeekStart)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, bytes.NewReader(c.bgdt))
	if err != nil {
		return err
	}

	return nil
}

func (c *Compiler) writeBlockBitmap(ctx context.Context, w io.WriteSeeker, g int64) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	_, err = w.Seek((g*c.blocksPerGroup+blocksPerSuperblock+c.blocksPerBGDT)*BlockSize, io.SeekStart)
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

func (c *Compiler) writeInodeBitmap(ctx context.Context, w io.WriteSeeker, g int64) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	_, err = w.Seek((g*c.blocksPerGroup+blocksPerSuperblock+c.blocksPerBGDT+blocksPerBlockBitmap)*BlockSize, io.SeekStart)
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

func (c *Compiler) setInodePointers(ino int64, inode *Inode) error {

	node := c.inodeBlocks[ino]
	start := int64(node.start)
	length := int64(node.fs)

	// direct pointers
	for i := int64(0); i < 12; i++ {
		if length <= i {
			return nil
		}
		inode.DirectPointer[i] = uint32(c.mapDBtoBlockAddr(start + i))
	}

	// singly indirect
	if length > 12 {
		inode.SinglyIndirect = uint32(c.mapDBtoBlockAddr(start + 12))
	} else {
		return nil
	}

	// doubly indirect
	refsPerBlock := int64(BlockSize / 4)
	if length > refsPerBlock+12+1 {
		inode.DoublyIndirect = uint32(c.mapDBtoBlockAddr(start + refsPerBlock + 12 + 1))
	} else {
		return nil
	}

	// triply indirect
	if length > 12+1+refsPerBlock+1+refsPerBlock+refsPerBlock*refsPerBlock {
		inode.TriplyIndirect = uint32(c.mapDBtoBlockAddr(start + 12 + 1 + refsPerBlock + 1 + refsPerBlock + refsPerBlock*refsPerBlock))
	}

	return nil
}

func (c *Compiler) writeInodeTable(ctx context.Context, w io.WriteSeeker, g int64) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	_, err = w.Seek((g*c.blocksPerGroup+blocksPerSuperblock+c.blocksPerBGDT+blocksPerBlockBitmap+blocksPerInodeBitmap)*BlockSize, io.SeekStart)
	if err != nil {
		return err
	}

	for i := int64(1); i <= c.inodesPerGroup; i++ {
		ino := i + g*c.inodesPerGroup
		inode := &Inode{}

		if int64(len(c.inodeBlocks)) <= ino || c.inodeBlocks[ino].node == nil {
			err := binary.Write(w, binary.LittleEndian, inode)
			if err != nil {
				return err
			}
			continue
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
		err = c.setInodePointers(ino, inode)
		if err != nil {
			return err
		}

		err = binary.Write(w, binary.LittleEndian, inode)
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
			// inode
			err := binary.Write(buf, binary.LittleEndian, uint32(0))
			if err != nil {
				return nil, err
			}
			// entry size
			err = binary.Write(buf, binary.LittleEndian, uint16(leftover))
			if err != nil {
				return nil, err
			}
			// name length
			err = binary.Write(buf, binary.LittleEndian, uint16(0))
			if err != nil {
				return nil, err
			}
			// padding
			_, err = buf.Write(bytes.Repeat([]byte{0}, int(leftover-8)))
			if err != nil {
				return nil, err
			}

			length += leftover
			length += l
			leftover = int64(BlockSize) - l
		}

		if leftover < 8 || i == len(tuples)-1 {
			l += leftover
			length += leftover
			leftover = int64(BlockSize)
		}

		// inode
		err := binary.Write(buf, binary.LittleEndian, child.inode)
		if err != nil {
			return nil, err
		}

		// entry size
		err = binary.Write(buf, binary.LittleEndian, uint16(l))
		if err != nil {
			return nil, err
		}

		// name length
		err = binary.Write(buf, binary.LittleEndian, uint16(len(child.name)))
		if err != nil {
			return nil, err
		}

		// name
		err = binary.Write(buf, binary.LittleEndian, append([]byte(child.name), 0))
		if err != nil {
			return nil, err
		}

		// padding
		_, err = buf.Write(bytes.Repeat([]byte{0}, int(l-8-int64(len(child.name))-1)))
		if err != nil {
			return nil, err
		}

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

func (c *Compiler) blockType(i int64) int {

	refsPerBlock := int64(BlockSize / pointerSize)

	singly := int64(12)
	singlySpan := 1 + refsPerBlock
	doubly := singly + singlySpan
	doublySpan := 1 + refsPerBlock*(refsPerBlock+1)
	triply := doubly + doublySpan

	// if single indirect block
	if i == singly {
		return 1
	}

	// if double indirect first-level block
	if i == doubly {
		return 2
	}

	// if double indirect second-level block
	if i > doubly && i < triply && (i-doubly-1)%singlySpan == 0 {
		return 1
	}

	// if triple indirect first-level block
	if i == triply {
		return 3
	}

	// if triple indirect third-level block
	if i > triply {
		i -= (triply + 1)
		if i%doublySpan == 0 {
			return 2
		}
		for ; i > doublySpan; i -= doublySpan {

		}
		i--
		if i%singlySpan == 0 {
			return 1
		}
	}

	return 0
}

func (c *Compiler) nextDataBlock(w io.Writer) error {

	var getNext func() error
	getNext = func() error {
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
				return getNext()
			}
		}
		return nil
	}

	err := getNext()
	if err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}

	// write next block
	buffer := new(bytes.Buffer)
	btype := c.blockType(c.activeNodeBlock)

	refsPerBlock := int64(BlockSize / pointerSize)
	var j int64

	switch btype {
	case 0:
		// it is a data block
		_, err := io.CopyN(buffer, c.activeNodeReader, BlockSize)
		if err != nil && err != io.EOF {
			return err
		}
	case 1:
		// it is a single indirect pointer block
		for j = c.activeNodeBlock + 1; j < c.activeNodeBlocks && j < refsPerBlock+c.activeNodeBlock+1; j++ {
			_ = binary.Write(buffer, binary.LittleEndian, uint32(c.mapDBtoBlockAddr(j+c.activeNodeStart)))
		}
	case 2:
		// it is a double indirect pointer block
		for j = c.activeNodeBlock + 1; j < c.activeNodeBlocks && j < c.activeNodeBlock+1+(1+refsPerBlock)*refsPerBlock; j = j + (1 + refsPerBlock) {
			_ = binary.Write(buffer, binary.LittleEndian, uint32(c.mapDBtoBlockAddr(j+c.activeNodeStart)))
		}
	case 3:
		// it is a triple indirect pointer block
		for j = c.activeNodeBlock + 1; j < c.activeNodeBlocks && j < c.activeNodeBlock+1+(1+refsPerBlock)*refsPerBlock+refsPerBlock*refsPerBlock*refsPerBlock; j = j + (1+refsPerBlock)*refsPerBlock + 1 {
			_ = binary.Write(buffer, binary.LittleEndian, uint32(c.mapDBtoBlockAddr(j+c.activeNodeStart)))
		}
	}
	// pad and write

	_, err = w.Write(buffer.Bytes())
	if err != nil {
		return err
	}
	c.activeNodeBlock++

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

func (c *Compiler) Compile(ctx context.Context, w io.WriteSeeker) error {

	var err error

	for g := int64(0); g < c.groups; g++ {

		// superblock
		err = c.writeSuperblock(ctx, w, g)
		if err != nil {
			return err
		}

		// bgdt
		err = c.writeBGDT(ctx, w, g)
		if err != nil {
			return err
		}

		// block bitmap
		err = c.writeBlockBitmap(ctx, w, g)
		if err != nil {
			return err
		}

		// inode bitmap
		err = c.writeInodeBitmap(ctx, w, g)
		if err != nil {
			return err
		}

		// inode table
		err = c.writeInodeTable(ctx, w, g)
		if err != nil {
			return err
		}

		// data
		err = c.writeDataBlocks(ctx, w, g)
		if err != nil {
			return err
		}

	}

	_, err = w.Seek(c.size, io.SeekStart)
	if err != nil {
		return err
	}

	return nil

}
