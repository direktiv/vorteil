package xfs

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	"github.com/davidminor/uint128"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vio"
)

type precompilerArgs struct {
	Logger   elog.Logger
	Contents vio.FileTree
	Options  struct {
		MinimumFreeInodes int64
		MinimumFreeSpace  int64
	}
}

type constants struct {
	exponents struct {
		sectorSize          uint8
		blockSize           uint8
		blocksPerAllocGroup uint8
		inodeSize           uint8
		directoryBlockSize  uint8
	}
	targetAllocGroupCount int64
	pageSize              int64
	inodeDataCapacity     int64
	allocGroups           int64
	journalBlocks         int64
	treeBlocks            int64

	usedInodes  int64
	freeInodes  int64
	totalInodes int64

	usedBlocks  int64
	freeBlocks  int64
	totalBlocks int64
}

// NOTE: these translation functions are a post-hoc hack to fix a bug I caused by not understanding XFS.
func (c *constants) translateRelativeInodeNumber(x int64) uint64 {
	ag := x / c.inodesPerAllocGroup()
	rel := x % c.inodesPerAllocGroup()
	rel += c.inodesPerBlock() * (c.superMetaBlocks() + 7)
	if ag == 0 {
		rel += c.journalBlocks * c.inodesPerBlock()
	}
	return uint64(rel)
}

var overrideInodeTranslator func(x int64) uint64

func (c *constants) translateAbsoluteInodeNumber(x int64) uint64 {

	if overrideInodeTranslator != nil {
		return overrideInodeTranslator(x)
	}

	ag := x / c.inodesPerAllocGroup()
	rel := x % c.inodesPerAllocGroup()
	rel += c.inodesPerBlock() * (c.superMetaBlocks() + 7)
	if ag == 0 {
		rel += c.journalBlocks * c.inodesPerBlock()
	}
	return c.inodeNumber(ag, rel)
}

func (c *constants) inodeNumber(ag int64, rel int64) uint64 {
	bits := c.exponents.blocksPerAllocGroup + (c.exponents.blockSize - c.exponents.inodeSize)
	return (uint64(ag) << bits) | uint64(rel)
}

func (c *constants) translateRelativeBlockNumber(x int64) uint64 {
	return uint64(x % c.blocksPerAllocGroup())
}

func (c *constants) translateAbsoluteBlockNumber(x int64) uint64 {
	ag := x / c.blocksPerAllocGroup()
	rel := x % c.blocksPerAllocGroup()
	return c.blockNumber(ag, rel)
}

func (c *constants) blockNumber(ag int64, rel int64) uint64 {
	bits := c.exponents.blocksPerAllocGroup
	return (uint64(ag) << bits) | uint64(rel)
}

func (c *constants) sectorSize() int64 {
	return 1 << c.exponents.sectorSize
}

func (c *constants) blockSize() int64 {
	return 1 << c.exponents.blockSize
}

func (c *constants) inodeSize() int64 {
	return 1 << c.exponents.inodeSize
}

func (c *constants) inodesPerBlock() int64 {
	return 1 << (c.exponents.blockSize - c.exponents.inodeSize)
}

func (c *constants) blocksPerAllocGroup() int64 {
	return 1 << c.exponents.blocksPerAllocGroup
}

func (c *constants) inodeBlocksPerAllocGroup() int64 { // TODO: unit test if it's possible to get an odd number out of this (I'm suspicious...)
	inodesPerAllocGroup := divide(c.totalInodes, c.allocGroups)
	inodesPerAllocGroup = align(inodesPerAllocGroup, 64) // round up to nearest 64
	return divide(inodesPerAllocGroup, c.inodesPerBlock())
}

func (c *constants) inodesPerAllocGroup() int64 {
	return c.inodeBlocksPerAllocGroup() * c.inodesPerBlock()
}

func (c *constants) directoryBlockSize() int64 {
	return c.blockSize() * (1 << c.exponents.directoryBlockSize)
}

func (c *constants) calculateInodes(used, minimumFree int64) {
	c.usedInodes = used
	c.freeInodes = minimumFree
	c.totalInodes = c.usedInodes + c.freeInodes
	c.totalInodes = c.inodesPerAllocGroup() * c.allocGroups
	c.freeInodes = c.totalInodes - c.usedInodes
}

func (c *constants) superMetaBlocks() int64 {
	return divide(4*c.sectorSize(), c.blockSize()) // 4 sectors
}

func (c *constants) metadataBlocksPerAllocGroup() int64 {
	return c.superMetaBlocks() + 7 + c.inodeBlocksPerAllocGroup() // + 7 blocks for b+ trees and the free list
}

func (c *constants) partialCalculateSpace() {
	c.usedBlocks = c.treeBlocks + c.journalBlocks
	metadataBlocks := c.metadataBlocksPerAllocGroup() * c.allocGroups
	c.usedBlocks += metadataBlocks
}

type precompiler struct {
	log elog.Logger
	constants
	args       precompilerArgs
	tree       vio.FileTree
	nodeBlocks []uint32 // uint32 gives us a file-size limit of 16 TiB with 4 KiB blocks
}

func (p *precompiler) setBlockSize(size int64) error {

	if size == 0 {
		p.exponents.blockSize = 12 // 4 KiB
		return nil
	}

	if size < p.sectorSize() {
		return fmt.Errorf("minimum block size is %d", p.sectorSize())
	}

	if size > p.pageSize {
		return fmt.Errorf("maximum block size is %d", p.pageSize)
	}

	for x, y := p.exponents.sectorSize, p.sectorSize(); y <= p.pageSize; x, y = x+1, 2*y {
		if y == size {
			p.exponents.blockSize = x
			break
		}
	}

	if p.exponents.blockSize == 0 {
		return errors.New("block size must be a power of two")
	}

	return nil
}

func (p *precompiler) calculateMinimumSize(ctx context.Context) error {

	var err error

	p.allocGroups = 1
	p.exponents.blocksPerAllocGroup = 12 // This makes for 16 MiB groups when using 4 KiB blocks. Big enough for a small journal log.

	// scan file tree to calculate space & inode requirements
	var blocks int64
	var k int
	treeNodes := p.tree.NodeCount()

	// realtime stuff
	var root *vio.TreeNode
	// blocks += 1
	treeNodes += 2
	p.nodeBlocks = make([]uint32, treeNodes)
	// p.nodeBlocks[2] = 1

	err = p.tree.WalkNode(func(path string, node *vio.TreeNode) error {

		if ctx.Err() != nil {
			return err
		}

		if root == nil {
			root = node
			root.Parent = root
		} else {
			node.NodeSequenceNumber += 2 // offset for realtime devices
			k = int(node.NodeSequenceNumber)
		}

		var x int64
		f := node.File

		if f.IsDir() {

			var ls int64 // length (short form)
			var lb int64 // length (block form)
			var ll int64 // length (leaf form)
			// TODO: var ln int64 // length (node form)

			ls = 6 - 17 // Header + negative for . & .. NOTE: the 6 could be 10 if we ever expect to have a huge number of inodes
			lb = 24     // Magic number, best free array, and block tail.
			ll = 16     // Magic number and best free array.

			grow := func(child string) {
				l := int64(len(child))
				ls += l + 7 // NOTE: this could be +11 if we ever expect to have a huge number of inodes
				l = 11 + l
				l = align(l, 8) // round up to nearest 8
				lb += l + 8     // +8: leaf entry

				// TODO: shuffle entries to optimize used space

				delta := align(ll, p.blockSize()) - ll
				if delta < l || delta-l < 16 {
					ll += delta + 16
				}

				ll += l
			}

			grow(".")
			grow("..")

			for _, child := range node.Children {
				grow(child.File.Name())
			}

			entries := int64(2 + len(node.Children))

			if ls < p.inodeDataCapacity {
				// directory can be stored in short form
				x = 0
			} else if lb < p.directoryBlockSize() {
				// directory can be stored in block form
				x = 1
			} else {
				// determine how many extents are necessary to store the data
				next := divide(ll, p.blockSize()) // number of extents for the directory data
				for {
					lfll := 16 + 4 + 2*next + (8 * entries) // leaf-form leaf length
					ddl := ll                               // directory data length
					ddb := divide(ddl, p.directoryBlockSize())
					// if ddb > 1 && next == 1 {
					// 	next = 2
					// 	continue
					// 	// NOTE: This is a hack for leaf-form directories. A leaf form directory
					// 	// cannot conceivably be part of more than two allocation groups in this
					// 	// implementation, but calculating how many extents are genuinely needed
					// 	// is challenging. The solution here is to assume it's spread over the
					// 	// maximum number of alloc groups.
					// }
					if lfll > p.directoryBlockSize() {
						// directory must be stored in node form
						x = ddb // data blocks
						x += 1  // node block

						leafBlocksBytes := 16 + (8 * entries)
						headerSize := int64(16)
						leafBlocks := divide(leafBlocksBytes, p.directoryBlockSize()-headerSize)
						// leafBlocksBytes += leafBlocks * headerSize
						x += leafBlocks // leaf blocks

						freeIndexBytes := 16 + 2*next
						freeIndexBlocks := divide(freeIndexBytes, p.directoryBlockSize())
						x += freeIndexBlocks // freeindex blocks
						break
					}
					x = 1 + ddb
					break
				}
			}
		} else if f.IsSymlink() {
			x = int64(f.Size())
			if f.SymlinkIsCached() && x < p.inodeDataCapacity {
				x = 0
			}
			x = divide(x, p.blockSize())
		} else {
			x = int64(f.Size())
			x = divide(x, p.blockSize())
		}

		if x >= 0x100000000 {
			return errors.New("object too large to include")
		}

		p.nodeBlocks[k] = uint32(x)
		blocks += x

		return nil
	})
	if err != nil {
		goto fail
	}

	p.treeBlocks = blocks
	p.journalBlocks = 1368 // no idea why, but this seems to be the smallest number the reference implementation uses

	k = -1

	for {
		k++
		if k > 0 {
			if k > 16 {
				err = errors.New("failed to pre-calculate minimum file-system size within a reasonable number of iterations")
				goto fail
			}
		}

		err = ctx.Err()
		if err != nil {
			goto cancel
		}

		// inodes
		p.calculateInodes(int64(treeNodes), p.args.Options.MinimumFreeInodes)

		// space
		minFreeBlocks := divide(p.args.Options.MinimumFreeSpace, p.blockSize())
		p.freeBlocks = minFreeBlocks
		p.partialCalculateSpace()
		p.totalBlocks = p.usedBlocks + p.freeBlocks

		minBlocks := (p.allocGroups-1)*p.blocksPerAllocGroup() + p.metadataBlocksPerAllocGroup()
		if p.totalBlocks < minBlocks {
			p.totalBlocks = minBlocks
			p.freeBlocks = p.totalBlocks - p.usedBlocks
		}

		//
		// reiteration checks
		//

		if p.totalBlocks > p.allocGroups*p.blocksPerAllocGroup() {
			// We need to grow the disk. We can achieve this by adding more alloc groups or increasing the size of alloc groups.
			// This logic attempts to strike a balance between the two, since larger alloc groups allow for bigger journals and
			// wastes less disk space on metadata, while more numerous alloc groups increases parallel performance.
			x := divide(p.totalBlocks, p.blocksPerAllocGroup())
			if x >= p.targetAllocGroupCount*2 {
				p.exponents.blocksPerAllocGroup++
				x = divide(p.totalBlocks, p.blocksPerAllocGroup())
			}
			p.allocGroups = x
			continue
		}

		// NOTE: If we want to ensure a good ratio between inode metadata and data blocks we should do it here.
		//		I have avoided doing so here because I think it might be counter-intuitive with Vorteil's "+X MiB" approach.

		if p.journalBlocks < p.blocksPerAllocGroup()-p.metadataBlocksPerAllocGroup() {
			// Check if we can grow the journal.
			growth := (p.blocksPerAllocGroup() - p.metadataBlocksPerAllocGroup()) - p.journalBlocks
			if x := p.freeBlocks - minFreeBlocks; x < growth {
				growth = x
			}
			if growth > 0 {
				p.journalBlocks += growth
				continue
			}
		}

		break
	}

	return nil

fail:
	if err == context.Canceled || err == context.DeadlineExceeded {
		goto cancel
	}

	return err

cancel:
	return err

}

func newBuild(ctx context.Context, args *precompilerArgs) (*precompiler, error) {

	var err error

	p := &precompiler{
		log: args.Logger,
		constants: constants{
			pageSize:              0x10000, // 64 KiB
			targetAllocGroupCount: 8,       // larger numbers might increase parallel performance, smaller numbers might increase journal size (which can also increase performance)
		},
		args: *args,
		tree: args.Contents,
	}
	p.exponents.sectorSize = 9 // 512 bytes
	p.exponents.inodeSize = 9  // 512 bytes
	p.inodeDataCapacity = p.inodeSize() - 100
	p.exponents.directoryBlockSize = 0

	err = p.setBlockSize(0x1000) // 4 KiB
	if err != nil {
		goto fail
	}

	if err = ctx.Err(); err != nil {
		goto cancel
	}

	err = p.calculateMinimumSize(ctx)
	if err != nil {
		goto fail
	}

	return p, nil

fail:
	if err == context.Canceled || err == context.DeadlineExceeded {
		goto cancel
	}

	return nil, err

cancel:
	return nil, err

}

func (p *precompiler) MinimumSize() int64 {
	return p.totalBlocks * p.blockSize()
}

type compiler struct {
	log elog.Logger
	constants
	args                      precompilerArgs
	tree                      vio.FileTree
	data, nodes               chan *vio.TreeNode
	dataError, nodesError     error
	dataReader                io.Reader
	dataReaderBlocksRemaining int64
	nodeCounter               int

	bitmap               []uint64
	nodeBlocks           []uint32
	nodeExtents          [][]*extent
	lastCalculatedNode   int64
	allocGroupFreeBlocks []int64
	allocGroupFreeInodes []int64

	countedNodeBlocks int64
}

func (p *precompiler) Precompile(ctx context.Context, fsSize int64) (*compiler, error) {

	var err error

	c := &compiler{
		log:                p.log,
		constants:          p.constants,
		args:               p.args,
		tree:               p.tree,
		nodeBlocks:         p.nodeBlocks,
		nodeExtents:        make([][]*extent, len(p.nodeBlocks)),
		lastCalculatedNode: -1,
	}

	if fsSize%p.blockSize() != 0 {
		return nil, fmt.Errorf("file-system must be a multiple of the block size (%v)", p.blockSize())
	}

	if fsSize < p.MinimumSize() {
		return nil, fmt.Errorf("file-system needs at least %v more space", vcfg.Bytes(p.MinimumSize()-fsSize))
	}

	if err = ctx.Err(); err != nil {
		goto cancel
	}

	c.totalBlocks = fsSize / p.blockSize()
	err = c.precompile(ctx)
	if err != nil {
		goto fail
	}

	return c, nil

fail:
	if err == context.Canceled || err == context.DeadlineExceeded {
		goto cancel
	}

	return nil, err

cancel:
	return nil, err

}

func (c *compiler) precompile(ctx context.Context) error {

	var err error

	// alloc group sizes
	for {
		c.allocGroups = divide(c.totalBlocks, c.blocksPerAllocGroup())
		if c.allocGroups < 2*c.targetAllocGroupCount {
			break
		}
		c.exponents.blocksPerAllocGroup++
	}

	c.calculateInodes(c.usedInodes, c.args.Options.MinimumFreeInodes)

	minFreeBlocks := divide(c.args.Options.MinimumFreeSpace, c.blockSize())
	k := -1

	for {
		k++
		if k > 16 {
			err = errors.New("failed to calculate space in a reasonable number of iterations")
			goto fail
		}

		c.partialCalculateSpace()
		c.freeBlocks = c.totalBlocks - c.usedBlocks

		// Check if we can grow the journal.
		if c.journalBlocks+1 < c.blocksPerAllocGroup()-c.metadataBlocksPerAllocGroup() {
			growth := (c.blocksPerAllocGroup() - c.metadataBlocksPerAllocGroup()) - c.journalBlocks
			if x := c.freeBlocks - minFreeBlocks; x < growth {
				growth = x
			}

			// NOTE: this prevents the inodes section from becoming misaligned
			if growth%2 > 0 {
				growth--
			}

			if growth > 0 {
				c.journalBlocks += growth
				continue
			}
		}

		break
	}

	if err = ctx.Err(); err != nil {
		goto cancel
	}

	// calculate use bitmap
	c.buildBitmap()

	return nil

fail:
	if err == context.Canceled || err == context.DeadlineExceeded {
		goto cancel
	}

	return err

cancel:
	return err

}

func (c *compiler) buildBitmap() {
	c.allocGroupFreeBlocks = make([]int64, c.allocGroups)
	c.allocGroupFreeInodes = make([]int64, c.allocGroups)

	for i := int64(0); i < c.allocGroups; i++ {
		c.allocGroupFreeBlocks[i] = c.blocksPerAllocGroup() - c.metadataBlocksPerAllocGroup()
		c.allocGroupFreeInodes[i] = c.inodesPerAllocGroup()
	}

	// trim space off the last alloc group
	if c.totalBlocks != c.blocksPerAllocGroup()*c.allocGroups {
		c.allocGroupFreeBlocks[c.allocGroups-1] = (c.totalBlocks % c.blocksPerAllocGroup()) - c.metadataBlocksPerAllocGroup()
	}

	// subtract space from first alloc group for the journal
	c.allocGroupFreeBlocks[0] -= c.journalBlocks

	// distribute nodes amongst alloc groups TODO: see if files can be spread out better amongst the alloc groups
	inodes := int64(len(c.nodeBlocks))
	idx := int64(0)
	for {
		delta := c.inodesPerAllocGroup()
		if delta >= inodes {
			c.allocGroupFreeInodes[idx] -= inodes
			inodes = 0
			break
		}
		c.allocGroupFreeInodes[idx] = 0
		inodes -= delta
		idx++
		if idx >= c.allocGroups {
			panic(errors.New("failed to distribute inode but it should have been possible"))
		}
	}

	idx = 0
	for _, nodeBlocks := range c.nodeBlocks {
		blocks := int64(nodeBlocks)
		for {
			delta := c.allocGroupFreeBlocks[idx]
			if delta >= blocks {
				c.allocGroupFreeBlocks[idx] -= blocks
				blocks = 0
				break
			}
			c.allocGroupFreeBlocks[idx] = 0
			blocks -= delta
			idx++
			if idx >= c.allocGroups {
				panic(errors.New("failed to distribute blocks but it should have been possible"))
			}
		}
	}

	// build the bitmap
	c.bitmap = make([]uint64, divide(c.totalBlocks, 64))
	for ag := int64(0); ag < c.allocGroups; ag++ {
		l := c.allocGroupFreeBlocks[ag]
		first := c.blocksPerAllocGroup() - l

		// exception for the final alloc group
		if ag == c.allocGroups-1 && c.totalBlocks != c.blocksPerAllocGroup()*c.allocGroups {
			first -= c.blocksPerAllocGroup() - (c.totalBlocks % c.blocksPerAllocGroup())
		}

		// TODO: improve performance here by treating uint64s instead of bits
		for i := int64(0); i < l; i++ {
			block := first + i
			chunk := block / 64
			bit := block % 64
			c.bitmap[chunk] |= (0x1 << bit)
		}
	}

}

func (c *compiler) Size() int64 {
	return c.blockSize() * c.totalBlocks
}

func (c *compiler) Compile(ctx context.Context, w io.WriteSeeker) error {

	var err error

	c.data = make(chan *vio.TreeNode)  // TODO: close this safely
	c.nodes = make(chan *vio.TreeNode) // TODO: close this safely
	go c.treeWalkers()

	defer func() {
		c.tree.Close()
		for {
			if _, more := <-c.data; !more {
				break
			}
		}
		for {
			if _, more := <-c.nodes; !more {
				break
			}
		}
	}()

	err = c.writeAllocGroups(ctx, w)
	if err != nil {
		goto fail
	}

	return nil

fail:
	if err == context.Canceled || err == context.DeadlineExceeded {
		goto cancel
	}

	return err

cancel:
	return err
}

func (c *compiler) writeAllocGroups(ctx context.Context, w io.WriteSeeker) error {
	var err error
	for ag := int64(0); ag < c.allocGroups; ag++ {
		err = c.writeAllocGroup(ctx, w, ag)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) writeAllocGroup(ctx context.Context, w io.WriteSeeker, ag int64) error {
	var err error

	allocGroupOffset := ag * c.blocksPerAllocGroup() * c.blockSize()
	_, err = w.Seek(allocGroupOffset, io.SeekStart)
	if err != nil {
		return err
	}

	uuid, err := generateUID()
	if err != nil {
		return err
	}

	// superblock
	sb := &SuperBlock{
		MagicNumber: SBMagicNumber,
		BlockSize:   uint32(c.blockSize()),
		DataBlocks:  uint64(c.totalBlocks),
		// UUID:                       [16]byte{0x65, 0x7b, 0x9b, 0x73, 0xfe, 0xa4, 0x44, 0x3e, 0x98, 0x51, 0x6b, 0x01, 0x55, 0x7f, 0xea, 0xab}, // should this be generated?
		UUID:                       uuid,
		LogStart:                   uint64(c.superMetaBlocks() + 7), // first non-metadata thing on the first alloc group
		RootInode:                  c.translateAbsoluteInodeNumber(0),
		RealtimeBitmapInode:        c.translateAbsoluteInodeNumber(1),
		RealtimeSummaryInode:       c.translateAbsoluteInodeNumber(2),
		RealtimeExtentBlocks:       1,
		AGBlocks:                   uint32(c.blocksPerAllocGroup()),
		AGCount:                    uint32(c.allocGroups),
		LogBlocks:                  uint32(c.journalBlocks),
		VersionNum:                 VersionNumber | VersionAlignBit | VersionNlinkBit | VersionLogV2Bit | VersionExtFlgBit | VersionDirV2Bit | VersionMoreBitsBit,
		SectorSize:                 SectorSize,
		InodeSize:                  uint16(c.inodeSize()),
		InodesPerBlock:             uint16(c.inodesPerBlock()),
		FSName:                     [12]byte{'x', 'f', 's', 0, 0, 0, 0, 0, 0, 0, 0, 0}, // should this be configurable?
		BlockSizeLogarithmic:       uint8(c.exponents.blockSize),
		SectorSizeLogarithmic:      sectorSizeLog,
		InodeSizeLogarithmic:       c.exponents.inodeSize,
		InodesPerBlockLogarithmic:  uint8(c.exponents.blockSize - c.exponents.inodeSize),
		AGBlocksLogarithmic:        uint8(c.exponents.blocksPerAllocGroup),
		InodesMaxPercentage:        inodesPercentage,
		InodesAllocated:            uint64(c.allocGroups * c.inodesPerAllocGroup()),
		InodesFree:                 uint64(c.freeInodes),
		DataFree:                   uint64(c.freeBlocks + 4*c.allocGroups), // NOTE: +4/ag is for the reserved free-list blocks, I think.
		DirectoryBlocksLogarithmic: dirBlockAllocLog,
		LogSectorSizeLogarithmic:   0,
		LogSectorSize:              0,
		MoreFeatures:               Version2LazySBCountBit,
		BadFeatures:                Version2LazySBCountBit,
		// UserQuotasInode:            0xffffffff,
		// GroupQuotasInode:           0xffffffff,
		InodeChunkAlignment: 2,
		LogStripeUnit:       1,
	}

	err = binary.Write(w, binary.BigEndian, sb)
	if err != nil {
		return err
	}

	// alloc group free block info
	_, err = w.Seek(allocGroupOffset+c.sectorSize(), io.SeekStart)
	if err != nil {
		return err
	}

	abtbAddr := uint32(c.superMetaBlocks()) + 1
	abtcAddr := abtbAddr + 1
	freeBlocks := uint32(c.allocGroupFreeBlocks[ag])

	agf := &AGF{
		Magic:       AGFMagicNumber,
		Version:     AGFVersion,
		SeqNo:       uint32(ag),
		Length:      uint32(c.blocksPerAllocGroup()),
		Roots:       [2]uint32{abtbAddr, abtcAddr},
		Levels:      [2]uint32{1, 1},
		FLFirst:     0,
		FLLast:      3,
		FLCount:     4,
		FreeBlocks:  freeBlocks,
		Longest:     uint32(c.allocGroupFreeBlocks[ag]),
		BTreeBlocks: 0,
	}

	if ag == c.allocGroups-1 && c.totalBlocks%c.blocksPerAllocGroup() != 0 {
		agf.Length = uint32(c.totalBlocks % c.blocksPerAllocGroup())
	}

	err = binary.Write(w, binary.BigEndian, agf)
	if err != nil {
		return err
	}

	// inode b+ tree info
	_, err = w.Seek(allocGroupOffset+c.sectorSize()*2, io.SeekStart)
	if err != nil {
		return err
	}

	inodeChunks := c.inodesPerAllocGroup() / 64
	chunksPerLeaf := (c.blockSize() - 16) / 16 // -16 for header, /16 for record size
	leavesPerNode := (c.blockSize() - 16) / 8  // -16 for header, /8 for key/pointer pairs
	leavesNeeded := inodeChunks / chunksPerLeaf
	level := uint32(1)
	if leavesNeeded > 1 {
		level++
		if leavesNeeded > leavesPerNode {
			return errors.New("deep b+ trees unimplemented") // this will trigger if we try to have more than 8323200 inodes per alloc group
		}
		return errors.New("deep b+ trees unimplemented") // this will trigger if we try to have more than 16320 inodes per alloc group
	}

	agi := &AGI{
		Magic:     AGIMagicNumber,
		Version:   AGIVersion,
		SeqNo:     uint32(ag),
		Length:    uint32(c.blocksPerAllocGroup()),
		Count:     uint32(c.inodesPerAllocGroup()), // TODO: - c.allocGroupFreeInodes[ag]), -- is it meant to be this way?
		Root:      uint32(c.superMetaBlocks()),
		Level:     level,
		FreeCount: uint32(c.allocGroupFreeInodes[ag]),
		NewIno:    uint32(c.translateRelativeInodeNumber(ag * c.inodesPerAllocGroup())),
		DirIno:    uint32(0xFFFFFFFF), // NOTE: according to the spec this is NULL (-1)
	}

	for i := 0; i < 64; i++ {
		agi.Unlinked[i] = 0xFFFFFFFF // NOTE: according to the spec this is NULL (-1)
	}

	if ag == c.allocGroups-1 && c.totalBlocks%c.blocksPerAllocGroup() != 0 {
		agi.Length = uint32(c.totalBlocks % c.blocksPerAllocGroup())
	}

	err = binary.Write(w, binary.BigEndian, agi)
	if err != nil {
		return err
	}

	// internal free list info
	_, err = w.Seek(allocGroupOffset+c.sectorSize()*3, io.SeekStart)
	if err != nil {
		return err
	}

	agfl := []uint32{uint32(c.superMetaBlocks()) + 3, uint32(c.superMetaBlocks()) + 4, uint32(c.superMetaBlocks()) + 5, uint32(c.superMetaBlocks()) + 6}

	err = binary.Write(w, binary.BigEndian, agfl)
	if err != nil {
		return err
	}

	err = binary.Write(w, binary.BigEndian, bytes.Repeat([]byte{0xFF}, int(c.sectorSize()-16)))

	// write inode b+ tree
	ibt := &BTreeSBlock{
		Magic:    IBTMagicNumber,
		Level:    0,
		NumRecs:  uint16(inodeChunks),
		LeftSIB:  0xFFFFFFFF,
		RightSIB: 0xFFFFFFFF,
	}

	_, err = w.Seek(allocGroupOffset+int64(agi.Root)*c.blockSize(), io.SeekStart)
	if err != nil {
		return err
	}

	err = binary.Write(w, binary.BigEndian, ibt)
	if err != nil {
		return err
	}

	for i := int64(0); i < inodeChunks; i++ {
		var used int64
		used = c.usedInodes - (ag*c.inodesPerAllocGroup() + i*64)
		if used < 0 {
			used = 0
		} else if used > 64 {
			used = 64
		}
		rec := &InodeBTRecord{
			StartIno:  uint32(c.translateRelativeInodeNumber(i*64 + ag*c.inodesPerAllocGroup())),
			FreeCount: uint32(64 - used),
			Free:      0xFFFFFFFFFFFFFFFF << used,
		}
		err = binary.Write(w, binary.BigEndian, rec)
		if err != nil {
			return err
		}
	}

	// write free space b+ trees
	abtb := &BTreeSBlock{
		Magic:    ABTBMagicNumber,
		Level:    0,
		NumRecs:  1,
		LeftSIB:  0xFFFFFFFF,
		RightSIB: 0xFFFFFFFF,
	}

	firstFreeBlock := agf.Length - freeBlocks
	var ar *AllocRecord

	if c.allocGroupFreeBlocks[ag] > 0 {
		ar = &AllocRecord{
			StartBlock: firstFreeBlock,
			BlockCount: freeBlocks,
		}
	} else {
		abtb.NumRecs = 0
	}

	_, err = w.Seek(allocGroupOffset+int64(agf.Roots[0])*c.blockSize(), io.SeekStart)
	if err != nil {
		return err
	}

	err = binary.Write(w, binary.BigEndian, abtb)
	if err != nil {
		return err
	}

	if ar != nil {
		err = binary.Write(w, binary.BigEndian, ar)
		if err != nil {
			return err
		}
	}

	_, err = w.Seek(allocGroupOffset+int64(agf.Roots[1])*c.blockSize(), io.SeekStart)
	if err != nil {
		return err
	}

	abtc := &BTreeSBlock{
		Magic:    ABTCMagicNumber,
		Level:    0,
		NumRecs:  abtb.NumRecs,
		LeftSIB:  0xFFFFFFFF,
		RightSIB: 0xFFFFFFFF,
	}

	err = binary.Write(w, binary.BigEndian, abtc)
	if err != nil {
		return err
	}

	if ar != nil {
		err = binary.Write(w, binary.BigEndian, ar)
		if err != nil {
			return err
		}
	}

	// free list
	// NOTE: free list should be completely empty for us so we can ignore it

	// journal
	remainder := int64(agf.Length) - c.metadataBlocksPerAllocGroup() // this is how many blocks we have left for copying data
	if ag == 0 {
		_, err = w.Seek(int64(sb.LogStart)*c.blockSize(), io.SeekStart)
		if err != nil {
			return err
		}

		recHeader := &XLogRecHeader{
			Magic:     XLogMagicNumber,
			Cycle:     1,
			Version:   2,
			Len:       uint32(c.sectorSize()),
			LSN:       0x100000000,
			TailLSN:   0x100000000,
			CRC:       0,
			PrevBlock: 0xFFFFFFFF,
			NumLogOps: 1,
			Fmt:       1,
			FSUUID:    sb.UUID,
			Size:      0x8000,
		}
		recHeader.CycleData[0] = 0xB0C0D0D0 // TODO: figure out what this is supposed to mean.

		err = binary.Write(w, binary.BigEndian, recHeader)
		if err != nil {
			return err
		}

		rec := &XLogRecord{
			TransactionID: 1,
			Length:        8,
			ClientID:      0xAA,   // client id (XFS_LOG)
			Flags:         0x20,   // flags (XLOG_UNMOUNT_TRANS)
			Unknown:       0x6E55, // TODO: Figure out what this is supposed to mean.
		}

		err = binary.Write(w, binary.BigEndian, rec)
		if err != nil {
			return err
		}

		remainder -= c.journalBlocks
	}

	// inodes
	for ino := int64(0); ino < c.inodesPerAllocGroup(); ino++ {

		inodeNumber := c.translateRelativeInodeNumber(ino + c.inodesPerAllocGroup()*ag)

		_, err = w.Seek(allocGroupOffset+int64(inodeNumber)*c.inodeSize(), io.SeekStart)
		if err != nil {
			return err
		}

		if ino < c.inodesPerAllocGroup()-c.allocGroupFreeInodes[ag] {
			rdr := c.popInode()
			if rdr == nil {
				return c.nodesError
			}

			_, err = io.CopyN(w, rdr, c.inodeSize())
			if err != nil && err != io.EOF {
				return err
			}
		} else {
			err = binary.Write(w, binary.BigEndian, &InodeCore{
				Magic:        InodeMagicNumber,
				Format:       uint8(ino),
				Version:      2,
				NextUnlinked: 0xFFFFFFFF,
			})
			if err != nil {
				return err
			}
		}
	}

	if err = ctx.Err(); err != nil {
		return err
	}

	_, err = w.Seek(allocGroupOffset+(int64(agf.Length)-remainder)*c.blockSize(), io.SeekStart)
	if err != nil {
		return err
	}

	// data & metadata
	for remainder > int64(freeBlocks) {
		rdr := c.popDataBlock()
		if rdr == nil {
			return c.dataError
		}

		_, err = io.CopyN(w, rdr, c.blockSize())
		if err != nil && err != io.EOF {
			return err
		}
		remainder--
	}

	if remainder != 0 { // force a write to the end of the alloc group just in case that matters
		_, err = w.Seek(allocGroupOffset+int64(agf.Length)*c.blockSize()-1, io.SeekStart)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, bytes.NewReader([]byte{0}))
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *compiler) inodeNumberFromNode(n *vio.TreeNode) uint64 {
	return c.translateAbsoluteInodeNumber(n.NodeSequenceNumber)
}

func (c *compiler) popDataBlock() io.Reader {

	for {
		if c.dataReaderBlocksRemaining > 0 {
			c.dataReaderBlocksRemaining--
			return c.dataReader
		}

		n, more := <-c.data
		if !more {
			return nil
		}

		c.dataReaderBlocksRemaining = int64(c.nodeBlocks[c.nodeCounter])

		c.nodeCounter++

		if c.dataReaderBlocksRemaining == 0 {
			continue
		}

		if n.File.IsDir() {
			c.dataReader = c.generateDirectoryBlockData(n, c.dataReaderBlocksRemaining)
		} else if n.File.IsSymlink() {
			// TODO: does this work?
			_ = c.computeNodeExtents(n.NodeSequenceNumber, &dataRange{blocks: c.dataReaderBlocksRemaining}) // called here to ensure things are computed in order
			c.dataReader = io.MultiReader(n.File, io.LimitReader(vio.Zeroes, c.dataReaderBlocksRemaining*c.blockSize()-int64(n.File.Size())))
		} else {
			_ = c.computeNodeExtents(n.NodeSequenceNumber, &dataRange{blocks: c.dataReaderBlocksRemaining}) // called here to ensure things are computed in order
			c.dataReader = io.MultiReader(n.File, io.LimitReader(vio.Zeroes, c.dataReaderBlocksRemaining*c.blockSize()-int64(n.File.Size())))
		}
	}
}

type extent struct {
	first  uint64
	length int64
	offset int64
}

//
func (c *compiler) translateBlock(x int64) (uint64, int64) { // return (blockAddr, maxExtentLength)
	block := int64(0)
	block += c.metadataBlocksPerAllocGroup()
	block += c.journalBlocks
	delta := c.blocksPerAllocGroup() - block
	for i := int64(0); true; i++ {
		if x < delta && delta > 0 {
			delta -= x
			block += x
			x = 0
			break
		} else {
			block += delta
			x -= delta
			block += c.metadataBlocksPerAllocGroup()
			delta = c.blocksPerAllocGroup() - c.metadataBlocksPerAllocGroup()
		}
	}

	return c.translateAbsoluteBlockNumber(block), delta
}

func (c *compiler) computeExtents(x int64, commit bool) []*extent {
	var extents = []*extent{}
	first := c.countedNodeBlocks
	length := x
	offset := int64(0)

	for length > 0 {
		e := &extent{}
		e.first, e.length = c.translateBlock(first)
		if e.length > length {
			e.length = length
		}
		first += e.length
		length -= e.length
		e.offset = offset
		offset += e.length
		extents = append(extents, e)
	}

	if commit {
		c.countedNodeBlocks += x
	}
	return extents
}

type dataRange struct {
	blocks int64
	offset int64
}

func (c *compiler) computeNodeExtents(ino int64, fragments ...*dataRange) []*extent {

	var e []*extent

	if e = c.nodeExtents[ino]; e != nil {
		c.nodeExtents[ino] = nil // free memory (this should never be needed more than twice)
		return e
	}

	if c.lastCalculatedNode >= ino {
		panic(errors.New("went backwards calculating node extents"))
	}

	if len(fragments) == 0 {
		panic(errors.New("must compute at least one extent fragment"))
	}

	for _, frag := range fragments {
		x := c.computeExtents(frag.blocks, true)
		offset := frag.offset
		for i := 0; i < len(x); i++ {
			x[i].offset = offset
			offset += x[i].length
		}
		e = append(e, x...)
	}

	c.lastCalculatedNode = ino
	c.nodeExtents[ino] = e
	return e
}

func (c *compiler) computeNodeExtentsDryrun(ino int64, fragments ...int64) []*extent {

	var e []*extent

	if e = c.nodeExtents[ino]; e != nil {
		return e
	}

	if c.lastCalculatedNode >= ino {
		panic(errors.New("went backwards calculating node extents"))
	}

	if len(fragments) == 0 {
		panic(errors.New("must compute at least one extent fragment"))
	}

	for _, frag := range fragments {
		e = append(e, c.computeExtents(frag, false)...)
	}

	// c.lastCalculatedNode = ino
	// c.nodeExtents[ino] = e
	return e
}

func (c *compiler) popInode() io.Reader {

	n, more := <-c.nodes
	if !more {
		return nil
	}

	var format uint8
	var mode uint16
	var nextents int32
	var size int64
	var nblocks uint64

	extents := make([]*extent, 0)
	nblocks = uint64(c.nodeBlocks[n.NodeSequenceNumber])
	var data []byte

	if n.File.IsDir() {
		mode = 0x4000 | 0700
		format = InodeFormatLocal
		if nblocks > 0 {
			format = InodeFormatExtents
		}
		size, data, extents = c.generateDirectory(n)
	} else if n.File.IsSymlink() {
		mode = 0xA000 | 0700
		format = InodeFormatLocal
		if nblocks > 0 {
			extents = c.computeNodeExtents(n.NodeSequenceNumber, &dataRange{blocks: int64(nblocks)})
			format = InodeFormatExtents
		}
		size = int64(n.File.Size())
	} else { // regular file
		mode = 0x8000 | 0700
		format = InodeFormatExtents
		size = int64(n.File.Size())
		if size > 0 {
			extents = c.computeNodeExtents(n.NodeSequenceNumber, &dataRange{blocks: int64(nblocks)})
		}
	}

	nextents = int32(len(extents))

	core := &InodeCore{
		Magic:   InodeMagicNumber,
		Mode:    mode,
		Version: 2,
		Format:  format,
		// Onlink:  uint16(n.Links),
		UID:     1000,
		GID:     1000,
		Nlink:   uint32(n.Links),
		Size:    size,
		NBlocks: nblocks,
		// ExtSize TODO: is this necessary?
		NExtents:     nextents,
		AFormat:      InodeFormatExtents,
		NextUnlinked: 0xFFFFFFFF,
	}

	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, core)
	if err != nil {
		panic(err)
	}

	// write data fork

	if format == InodeFormatExtents {
		maxExtents := c.inodeDataCapacity / 16
		if int64(nextents) > maxExtents {
			panic(errors.New("super nested extents not yet supported")) // TODO
		}
		for _, e := range extents {

			var xe, xoffset, xnumber, xblocks uint128.Uint128

			xblocks.L = uint64(e.length)
			xblocks.L &= 0x1FFFFF
			xblocks = xblocks.ShiftLeft(0)

			xnumber.L = uint64(e.first)
			xnumber.L &= 0x0FFFFFFFFFFFFF
			xnumber = xnumber.ShiftLeft(21)

			xoffset.L = uint64(e.offset)
			xoffset.L &= 0x3FFFFFFFFFFFFF
			xoffset = xoffset.ShiftLeft(73)

			xe = xblocks.Or(xnumber).Or(xoffset)

			err = binary.Write(buf, binary.BigEndian, xe)
			if err != nil {
				panic(err)
			}

			fpath := n.File.Name()
			npath := n
			for {
				if npath.Parent == nil || npath.Parent == npath {
					break
				}
				npath = npath.Parent
				fpath = npath.File.Name() + "/" + fpath
			}

			// TODO: check this (I'm suspicious of the quality of my work when I wrote it)
		}
	} else if format == InodeFormatLocal {
		if n.File.IsDir() {
			_, err = io.Copy(buf, bytes.NewReader(data))
			if err != nil {
				panic(err)
			}
		} else if n.File.IsSymlink() {
			_, err := io.Copy(buf, strings.NewReader(n.File.Symlink()))
			if err != nil {
				panic(err) // NOTE: this is only supported if the filetree supports looking up and caching this value in advance, which should make errors impossible
			}
		} else {
			panic(errors.New("attempted to write local-format inode for an unsupported file-type"))
		}
	} else {
		panic(errors.New("attempted to write an unsupported inode format"))
	}

	return bytes.NewReader(buf.Bytes())

}

func (c *compiler) treeWalkers() {

	// realtime bitmap
	rtbmap := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			Size:       0,
			ReadCloser: ioutil.NopCloser(bytes.NewReader([]byte{})),
		}),
		NodeSequenceNumber: 1,
		Links:              1,
	}

	// realtime summary
	rtsummary := &vio.TreeNode{
		File: vio.CustomFile(vio.CustomFileArgs{
			Size:       0,
			ReadCloser: ioutil.NopCloser(bytes.NewReader([]byte{})),
		}),
		NodeSequenceNumber: 2,
		Links:              1,
	}

	// one walker for data blocks
	go func() {
		var previous *vio.TreeNode
		c.dataError = c.tree.WalkNode(func(path string, n *vio.TreeNode) error {
			c.data <- n
			if previous != nil && previous.File != nil {
				err := previous.File.Close()
				if err != nil {
					return err
				}
			}
			previous = n

			// insert realtime nodes
			if path == "." {
				c.data <- rtbmap
				c.data <- rtsummary
			}

			return nil
		})
		close(c.data)
	}()

	// one walker for inode information
	go func() {
		c.nodesError = c.tree.WalkNode(func(path string, n *vio.TreeNode) error {
			c.nodes <- n

			// insert realtime inodes
			if path == "." {
				c.nodes <- rtbmap
				c.nodes <- rtsummary
			}

			return nil
		})
		close(c.nodes)
	}()

}
