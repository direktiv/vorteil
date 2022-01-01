package ext4

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"time"
)

const (
	Signature    = 0xEF53
	RootDirInode = 2
	JournalInode = 8
)

const (
	SectorSize          = 512
	BlockSize           = 0x1000
	BlocksPerGroup      = BlockSize * 8
	DescriptorSize      = 32
	InodeSize           = 128
	SectorsPerBlock     = BlockSize / SectorSize
	DescriptorsPerBlock = BlockSize / DescriptorSize
	InodesPerBlock      = BlockSize / InodeSize
	PreallocFileBlocks  = 0
	PreallocDirBlocks   = 0
	SuperUID            = 1000
	SuperGID            = 1000
	MaxGroupDescriptors = (BlocksPerGroup / 2) * DescriptorsPerBlock // NOTE: capped to 128 TiB because we're not using meta block groups (META_BG) this is half the documented limit, but bg 0 can't be 100% group descriptors so I've cut it in half for safety
)

const (
	CompatDirPrealloc  = 0x1   // COMPAT_DIR_PREALLOC
	CompatHasJournal   = 0x4   // COMPAT_HAS_JOURNAL
	CompatResizeInode  = 0x10  // COMPAT_RESIZE_INODE
	CompatDirIndex     = 0x20  // COMPAT_DIR_INDEX
	CompatSparseSuper2 = 0x200 // COMPAT_SPARSE_SUPER2
)

const (
	IncompatFiletype   = 0x2    // INCOMPAT_FILETYPE
	IncompatExtents    = 0x40   // INCOMPAT_EXTENTS
	IncompatFlexBG     = 0x200  // INCOMPAT_FLEX_BG
	IncompatInlineData = 0x8000 // INCOMPAT_INLINE_DATA
)

const (
	ROCompatSparseSuper = 0x1 // RO_COMPAT_SPARSE_SUPER
	ROCompatLargeFile   = 0x2 // RO_COMPAT_LARGE_FILE
)

// Superblock is the structure of a superblock as written to the disk.
type Superblock struct {
	TotalInodes         uint32
	TotalBlocks         uint32
	_                   uint32
	UnallocatedBlocks   uint32
	UnallocatedInodes   uint32 // 0x10
	_                   uint32
	LogBlockSize        uint32
	LogClusterSize      uint32
	BlocksPerGroup      uint32 // 0x20
	ClustersPerGroup    uint32
	InodesPerGroup      uint32
	LastMountTime       uint32
	LastWrittenTime     uint32 // 0x30
	_                   uint16
	MountsCheckInterval uint16
	Signature           uint16
	State               uint16
	ErrorProtocol       uint16
	VersionMinor        uint16
	TimeLastCheck       uint32 // 0x40
	TimeCheckInterval   uint32
	_                   uint32
	VersionMajor        uint32
	ResUID              uint16 // 0x50
	ResGID              uint16
	FirstIno            uint32
	InodeSize           uint16
	BlockGroupNumber    uint16
	FeatureCompat       uint32
	FeatureIncompat     uint32 // 0x60
	FeatureROCompat     uint32
	UUID                [16]byte
	_                   [16]byte
	_                   [64]byte
	_                   uint32
	PreallocBlocks      uint8
	PreallocDirBlocks   uint8
	ReservedGDTBlocks   uint16
	JournalUUID         [16]byte // 0xD0
	JournalInum         uint32
	_                   uint32
	_                   uint32
	HashSeed            [4]uint32
	DefHashVersion      uint8
	JnlBackupType       uint8
	DescSize            uint16
	DefaultMountOpts    uint32 // 0x100
	_                   uint32
	_                   uint32
	_                   [17]uint32
	_                   uint32
	_                   uint32
	_                   uint32
	_                   uint16
	_                   uint16
	Flags               uint32 // 0x160
	_                   uint16
	_                   uint16
	_                   uint64
	_                   uint32
	LogGroupsPerFlex    uint8
	ChecksumType        uint8
	_                   uint16
	_                   uint64
	_                   uint32
	_                   uint32
	_                   uint64
	_                   uint32
	_                   uint32
	_                   uint32
	_                   uint32
	_                   uint64
	_                   [32]uint8
	_                   uint32
	_                   uint32
	_                   uint32
	_                   uint32
	_                   uint64
	_                   [32]uint8
	MountOptions        [64]uint8 // 0x200
	_                   uint32
	_                   uint32
	_                   uint32
	BackupBGs           [2]uint32
	_                   [4]uint8
	_                   [16]uint8
	_                   uint32
	_                   uint32
	_                   uint32
	_                   uint8
	_                   uint8
	_                   uint8
	_                   uint8
	_                   uint8
	_                   uint8
	_                   [2]uint8
	_                   uint16
	_                   uint16
	_                   [95]uint32
	_                   uint32
}

type layout struct {
	totalGroupDescriptors int64
	logGroupsPerFlex      int64
	totalBlocks           int64
	inodesPerGroup        int64
}

func (l *layout) groupsPerFlex() int64 {
	return int64(1) << l.logGroupsPerFlex
}

func (l *layout) totalGroups() int64 {
	return divide(l.totalBlocks, BlocksPerGroup)
}

func (l *layout) totalFlexes() int64 {
	return divide(l.totalGroups(), l.groupsPerFlex())
}

func (l *layout) totalInodes() int64 {
	return l.totalGroups() * l.inodesPerGroup
}

func (l *layout) reservedGDTBlocksPerTable() int64 {
	return divide(l.totalGroupDescriptors, DescriptorsPerBlock) - divide(l.totalGroups(), DescriptorsPerBlock)
}

func (l *layout) inodeBlocksPerGroup() int64 {
	return divide(l.inodesPerGroup, InodesPerBlock)
}

func (l *layout) superOverheadBlocks() int64 {
	return 1 + divide(l.totalGroupDescriptors, DescriptorsPerBlock)
}

func (l *layout) flexOverheadBlocks() int64 {
	return l.groupsPerFlex() * (2 + l.inodeBlocksPerGroup())
}

type descriptor struct {
	freeInodes  uint16
	freeBlocks  uint16
	directories uint16
}

type descriptors []descriptor

type super struct {
	layout
	timestamp time.Time

	descriptors      descriptors
	blockUsageBitmap []uint64
	inodes           *[]node
}

func totalGroupsAllowingForGrowth(g int64) int64 {
	g *= 1024
	g = align(g, DescriptorsPerBlock)
	if g > MaxGroupDescriptors {
		g = MaxGroupDescriptors
	}
	return g
}

func logGroupsPerFlex(totalGroups, inodeBlocksPerGroup, superOverheadBlocks int64) int64 {

	var x int64
	if totalGroups > 1 {
		x++
	}

	for {
		gpf := int64(1 << (x + 1))
		if totalGroups%gpf != 0 {
			break
		}

		// break out if the overhead for the first flex group will no longer fit in block group 0
		if (superOverheadBlocks + gpf*(2+inodeBlocksPerGroup)) >= BlocksPerGroup {
			break
		}
		x++
	}

	return x

}

func (s *super) extentTreeBlocks(n *node) int64 {

	if int64(n.content) < (3*32768 + 2) {
		return 0 // if the content does not exceed this size there's no way we need a deep extent tree
	}

	x := numberOfExtents(n, s)
	if x < 5 {
		return 0
	}

	return 1

}

func (s *super) init(totalBlocks, inodesPerGroup int64, nodes *[]node) {
	s.timestamp = time.Now()
	s.layout.inodesPerGroup = inodesPerGroup
	s.layout.totalBlocks = totalBlocks
	s.layout.totalGroupDescriptors = totalGroupsAllowingForGrowth(divide(totalBlocks, BlocksPerGroup))
	s.layout.logGroupsPerFlex = logGroupsPerFlex(s.totalGroups(), s.inodeBlocksPerGroup(), s.superOverheadBlocks())

	s.descriptors = make(descriptors, s.totalGroups())

	// correct negative start positions and count directories
	// also make adjustments for files with deep extent trees
	var correction int64

	for i := int64(1); i < 11; i++ {
		node := &(*nodes)[i]
		if node.start < correction {
			correction = node.start
		}
	}

	correction *= -1

	for i := range *nodes {

		idx := int64(i-1) / inodesPerGroup
		node := &(*nodes)[i]

		node.start += correction

		if node.node == nil {
			continue
		}

		if node.node.File.IsDir() {
			s.descriptors[idx].directories++
		}

		// deep extent trees adjustment
		extentTreeAdjustment := s.extentTreeBlocks(node)
		node.fs += uint32(extentTreeAdjustment)
		correction += extentTreeAdjustment
	}

	// set freeInodes and freeBlocks

	usedInodes := int64(len(*nodes) - 1) // -1 because inodes start counting at one instead of zero
	dataBlocks := (*nodes)[usedInodes].start + int64((*nodes)[usedInodes].fs)
	groupsPerFlex := s.groupsPerFlex()

	for i := int64(0); i < int64(len(s.descriptors)); i++ {

		// freeBlocks
		s.descriptors[i].freeBlocks = BlocksPerGroup
		if BlocksPerGroup*(i+1) > s.totalBlocks {
			s.descriptors[i].freeBlocks = uint16(s.totalBlocks - BlocksPerGroup*i)
		}
		if i == 0 {
			s.descriptors[i].freeBlocks -= uint16(s.superOverheadBlocks())
		}
		if i%groupsPerFlex == 0 {
			s.descriptors[i].freeBlocks -= uint16(s.flexOverheadBlocks())
		}
		delta := dataBlocks
		if delta > int64(s.groupFreeBlocks(i)) {
			delta = int64(s.groupFreeBlocks(i))
		}
		dataBlocks -= delta
		s.descriptors[i].freeBlocks -= uint16(delta)

		// freeInodes
		if usedInodes < inodesPerGroup*i {
			s.descriptors[i].freeInodes = uint16(inodesPerGroup)
		} else if usedInodes < (inodesPerGroup * (i + 1)) {
			s.descriptors[i].freeInodes = uint16(inodesPerGroup*(i+1) - usedInodes)
		}

	}

	s.fillBlockUsageBitmap(nodes)
	s.inodes = nodes

}

func (s *super) groupFreeBlocks(g int64) int64 {
	return int64(s.descriptors[g].freeBlocks)
}

func (s *super) totalFreeBlocks() int64 {
	var x int64
	for i := range s.descriptors {
		x += int64(s.groupFreeBlocks(int64(i)))
	}
	return x
}

func (s *super) groupFreeInodes(g int64) int64 {
	return int64(s.descriptors[g].freeInodes)
}

func (s *super) totalFreeInodes() int64 {
	var x int64
	for i := range s.descriptors {
		x += int64(s.descriptors[i].freeInodes)
	}
	return x
}

func (s *super) generateSuperblock(g int64) *Superblock {

	sb := &Superblock{
		TotalInodes:         uint32(s.totalInodes()),
		TotalBlocks:         uint32(s.totalBlocks),
		UnallocatedBlocks:   uint32(s.totalFreeBlocks()),
		UnallocatedInodes:   uint32(s.totalFreeInodes()),
		LogBlockSize:        2,
		LogClusterSize:      2,
		BlocksPerGroup:      BlocksPerGroup,
		ClustersPerGroup:    BlocksPerGroup,
		InodesPerGroup:      uint32(s.inodesPerGroup),
		LastMountTime:       uint32(s.timestamp.Unix()),
		LastWrittenTime:     uint32(s.timestamp.Unix()),
		MountsCheckInterval: 20,
		Signature:           Signature,
		State:               1,
		ErrorProtocol:       3,
		VersionMinor:        0,
		TimeLastCheck:       uint32(s.timestamp.Unix()),
		TimeCheckInterval:   0,
		VersionMajor:        1,
		ResUID:              SuperUID,
		ResGID:              SuperGID,
		FirstIno:            11,
		InodeSize:           InodeSize,
		BlockGroupNumber:    uint16(g),
		FeatureCompat:       CompatDirPrealloc | CompatHasJournal | CompatResizeInode | CompatDirIndex | CompatSparseSuper2,
		FeatureIncompat:     IncompatFiletype | IncompatExtents | IncompatFlexBG | IncompatInlineData,
		FeatureROCompat:     ROCompatSparseSuper | ROCompatLargeFile, // NOTE: the resize inode is "larger than 2 GiB"...
		// TODO UUID
		PreallocBlocks:    PreallocFileBlocks,
		PreallocDirBlocks: PreallocDirBlocks,
		ReservedGDTBlocks: uint16(s.reservedGDTBlocksPerTable()),
		// TODO JournalUUID
		JournalInum: JournalInode,
		// TODO HashSeed
		DefHashVersion: DirentHashVersion,
		// TODO JnlBackupType
		DescSize: DescriptorSize,
		// TODO DefaultMountOpts
		Flags:            0x2,
		LogGroupsPerFlex: uint8(s.logGroupsPerFlex),
		ChecksumType:     1,
		// TODO MountOptions
		// BackupBGs intentionally left blank (no redundancy).

	}

	return sb

}

func (s *super) writeSuperblock(w io.WriteSeeker, g int64) error {

	sb := s.generateSuperblock(g)
	offset := int64(1024)
	if g > 0 {
		offset = g * BlocksPerGroup * BlockSize
	}

	_, err := w.Seek(offset, io.SeekStart)
	if err != nil {
		return err
	}

	err = binary.Write(w, binary.LittleEndian, sb)
	if err != nil {
		return err
	}

	return nil

}

// BlockGroupDescriptor is the structure of a single block group descriptor as written to the disk.
type BlockGroupDescriptor struct {
	BlockBitmapAddr uint32 // 0x0
	InodeBitmapAddr uint32 // 0x4
	InodeTableAddr  uint32 // 0x8
	FreeBlocks      uint16 // 0xC
	FreeInodes      uint16 // 0xE
	Directories     uint16 // 0x10
	Flags           uint16 // 0x12
	_               uint32 // 0x14
	_, _            uint16 // 0x18
	UnusedInodes    uint16 // 0x1C
	_               uint16 // 0x1E
} // 0x20

func (s *super) generateBGDT() []byte {

	buf := new(bytes.Buffer)
	groups := s.totalGroups()
	groupsPerFlex := s.groupsPerFlex()
	inodeBlocksPerGroup := s.inodeBlocksPerGroup()
	superOverheadBlocks := s.superOverheadBlocks()

	var desc BlockGroupDescriptor

	for g := int64(0); g < groups; g++ {
		flex := g / groupsPerFlex
		remainder := g % groupsPerFlex
		delta := int64(0)
		if flex == 0 {
			delta += superOverheadBlocks
		}
		desc.BlockBitmapAddr = uint32(flex*groupsPerFlex*BlocksPerGroup + delta + remainder)
		desc.InodeBitmapAddr = desc.BlockBitmapAddr + uint32(groupsPerFlex)
		desc.InodeTableAddr = uint32(flex*groupsPerFlex*BlocksPerGroup + delta + 2*groupsPerFlex + remainder*inodeBlocksPerGroup)
		desc.FreeBlocks = uint16(s.groupFreeBlocks(g))
		desc.FreeInodes = uint16(s.groupFreeInodes(g))
		desc.Directories = s.descriptors[g].directories

		err := binary.Write(buf, binary.LittleEndian, &desc)
		if err != nil {
			panic(err)
		}
	}

	return buf.Bytes()

}

func (s *super) writeBGDT(w io.WriteSeeker, g int64) error {

	bgdt := s.generateBGDT()
	offset := (g*BlocksPerGroup + 1) * BlockSize

	_, err := w.Seek(offset, io.SeekStart)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, bytes.NewReader(bgdt))
	if err != nil {
		return err
	}

	return nil

}

func (s *super) writeSuperblockAndBGDT(ctx context.Context, w io.WriteSeeker, g int64) error {

	var err error
	err = ctx.Err()
	if err != nil {
		return err
	}

	err = s.writeSuperblock(w, g)
	if err != nil {
		return err
	}

	err = s.writeBGDT(w, g)
	if err != nil {
		return err
	}

	return nil

}

func (s *super) mapContentAddr(block int64) int64 {
	flexOverheadBlocks := s.flexOverheadBlocks()
	zeroth := s.groupsPerFlex() * BlocksPerGroup
	if s.totalBlocks < zeroth {
		zeroth = s.totalBlocks
	}
	zeroth -= s.superOverheadBlocks()
	zeroth -= s.flexOverheadBlocks()
	if block < zeroth {
		return s.superOverheadBlocks() + flexOverheadBlocks + block
	}

	groupsPerFlex := s.groupsPerFlex()

	block -= zeroth
	flexContent := groupsPerFlex*BlocksPerGroup - flexOverheadBlocks
	flex := 1 + block/flexContent
	delta := block % flexContent
	return flex*(groupsPerFlex*BlocksPerGroup) + flexOverheadBlocks + delta
}

func (s *super) mapContent(block int64) (addr int64, max int64) {
	addr = s.mapContentAddr(block)
	max = align(addr, s.groupsPerFlex()*BlocksPerGroup)
	if max > s.totalBlocks {
		max = s.totalBlocks
	}
	max = max - addr
	return
}

func (s *super) fillBlockUsageBitmap(nodes *[]node) {

	s.blockUsageBitmap = make([]uint64, divide(s.totalBlocks, 64))

	// data is packed in compactly from low addresses to high addresses sequentially
	// calculate first available data block so we can fill the block usage bitmap efficiently
	filledDataBlocks := (*nodes)[len(*nodes)-1].start + int64((*nodes)[len(*nodes)-1].fs)
	bno := s.mapContentAddr(filledDataBlocks)
	for i := int64(0); i < bno/64; i++ {
		s.blockUsageBitmap[i] = 0xFFFFFFFFFFFFFFFF
	}

	i := bno / 64
	j := bno % 64
	s.blockUsageBitmap[i] = 0xFFFFFFFFFFFFFFFF >> (64 - j)

	// manually insert overhead bits for subsequent groups
	flex := bno / (s.groupsPerFlex() * BlocksPerGroup)
	for x := flex + 1; x < s.totalFlexes(); x++ {
		for y := int64(0); y < s.flexOverheadBlocks(); y++ {
			bno = x*(s.groupsPerFlex()*BlocksPerGroup) + y
			i = bno / 64
			j = bno % 64
			s.blockUsageBitmap[i] |= 1 << j
		}
	}

	// mark bits for overhang in the final group
	for bno := s.totalBlocks; bno < int64(len(s.blockUsageBitmap)*64); bno++ {
		i = bno / 64
		j = bno % 64
		s.blockUsageBitmap[i] |= 1 << j
	}

}

func (s *super) regionIsHole(begin, size int64) bool {

	first := begin / BlockSize
	end := begin + size
	last := (end - 1) / BlockSize

	for bno := first; bno <= last; bno++ {

		i := bno / 64
		j := bno % 64

		if int(i) < len(s.blockUsageBitmap) && (s.blockUsageBitmap[i]&(0x1<<j)) > 0 {
			return false
		}

	}

	return true

}

func (s *super) writeBlockBitmap(w io.Writer, g int64) error {

	var err error
	var slice []uint64
	first := (BlocksPerGroup * g) / 64
	l := int64(BlocksPerGroup) / 64

	if g >= s.totalGroups() {
		goto end
	}

	slice = s.blockUsageBitmap[first:]
	if int64(len(slice)) > l {
		slice = slice[:l]
	}

	err = binary.Write(w, binary.LittleEndian, slice)
	if err != nil {
		return err
	}

end:
	var free int
	for _, x := range slice {
		for bit := 0; bit < 64; bit++ {
			if x&(x<<bit) == 0 {
				free++
			}
		}
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

func (s *super) writeInodeBitmap(w io.Writer, g int64) error {

	var err error

	bitmap := bytes.Repeat([]byte{0xFF}, BlockSize)

	if g < s.totalGroups() {
		free := int64(s.groupFreeInodes(g))
		for i := s.inodesPerGroup - free; i < s.inodesPerGroup; i++ {
			x := i / 8
			y := i % 8
			bitmap[x] &^= 0x1 << y
		}
	}

	err = binary.Write(w, binary.LittleEndian, bitmap)
	if err != nil {
		return err
	}

	return nil

}

func (s *super) writeInodeTable(w io.Writer, g int64) error {

	var err error

	for i := int64(1); i <= s.inodesPerGroup; i++ {

		ino := i + g*s.inodesPerGroup
		if ino >= int64(len(*s.inodes)) {
			break
		}

		node := &(*s.inodes)[ino]
		if node.node == nil {
			node = nil
		}

		inode := generateInode(node, s)
		if ino == ResizeInode && node != nil {
			inode = s.generateResizeInode(node, s)
		}

		err = binary.Write(w, binary.LittleEndian, inode)
		if err != nil {
			return err
		}

	}

	return nil

}

func (s *super) writeFlexGroupMetaData(ctx context.Context, w io.WriteSeeker, flex int64) error {

	var err error
	begin := flex * s.groupsPerFlex()
	end := (flex + 1) * s.groupsPerFlex()

	for g := begin; g < end; g++ {
		err = s.writeBlockBitmap(w, g)
		if err != nil {
			return err
		}
	}

	for g := begin; g < end; g++ {
		err = s.writeInodeBitmap(w, g)
		if err != nil {
			return err
		}
	}

	for g := begin; g < end; g++ {

		err = s.writeInodeTable(w, g)
		if err != nil {
			return err
		}
	}

	return nil

}
