package xfs

import (
	"bytes"
	"encoding/binary"
	"io"
	"sort"
	"strings"

	"github.com/vorteil/vorteil/pkg/vio"
)

type inodeTranslator interface {
	inodeNumberFromNode(n *vio.TreeNode) uint64
}

type shortDirHeader struct {
	TotalEntries uint8
	_            uint8
	ParentIno    uint32
}

func generateShortFormDentry(name string, ino, offset int64) (data []byte, delta int64) {

	buf := new(bytes.Buffer)
	l := len(name)

	err := binary.Write(buf, binary.BigEndian, uint8(l))
	if err != nil {
		panic(err)
	}

	err = binary.Write(buf, binary.BigEndian, uint16(offset))
	if err != nil {
		panic(err)
	}

	_, err = io.Copy(buf, strings.NewReader(name))
	if err != nil {
		panic(err)
	}

	err = binary.Write(buf, binary.BigEndian, uint32(ino)) // TODO: what if the translated number > 32 bit?
	if err != nil {
		panic(err)
	}

	return buf.Bytes(), align(12+int64(l), 8)

}

func generateShortFormDirectoryData(t inodeTranslator, n *vio.TreeNode) []byte {

	buf := new(bytes.Buffer)
	hdr := &shortDirHeader{
		TotalEntries: uint8(len(n.Children)),
		ParentIno:    uint32(t.inodeNumberFromNode(n.Parent)),
	}

	err := binary.Write(buf, binary.BigEndian, hdr)
	if err != nil {
		panic(err)
	}

	offset := int64(48) // virtual offset for 16 bytes of directory block header, 16 for '.', 16 for '..'

	for _, child := range n.Children {

		ino := t.inodeNumberFromNode(child)

		if ino>>32 > 0 {
			panic("superlarge inode in shortform directory")
		}

		dentry, delta := generateShortFormDentry(child.File.Name(), int64(ino), offset)

		_, err = io.Copy(buf, bytes.NewReader(dentry))
		if err != nil {
			panic(err)
		}

		offset += delta

	}

	return buf.Bytes()

}

func processDir2BlockFreeSpace(w io.Writer, header *Dir2Header, offset, space, blockSize int64) uint16 {

	if space <= 0 {
		return 0
	}

	header.BestFree[0].Offset = uint16(offset % blockSize)
	header.BestFree[0].Length = uint16(space)

	err := binary.Write(w, binary.BigEndian, uint16(0xFFFF))
	if err != nil {
		panic(err)
	}

	err = binary.Write(w, binary.BigEndian, uint16(space))
	if err != nil {
		panic(err)
	}

	_, err = io.CopyN(w, vio.Zeroes, space-6)
	if err != nil {
		panic(err)
	}

	err = binary.Write(w, binary.BigEndian, uint16(offset%blockSize))
	if err != nil {
		panic(err)
	}

	return header.BestFree[0].Length

}

func hashname(name string) uint32 {

	var hash uint32

	rol32 := func(word uint32, shift int) uint32 {
		return (word << (shift & 31)) | (word >> ((-shift) & 31))
	}

	for {
		switch len(name) {
		case 0:
			return hash
		case 1:
			hash = (uint32(name[0]) << 0) ^ rol32(hash, 7*1)
			name = name[1:]
		case 2:
			hash = (uint32(name[0]) << 7) ^ (uint32(name[1]) << 0) ^ rol32(hash, 7*2)
			name = name[2:]
		case 3:
			hash = (uint32(name[0]) << 14) ^ (uint32(name[1]) << 7) ^ (uint32(name[2]) << 0) ^ rol32(hash, 7*3)
			name = name[3:]
		default:
			hash = (uint32(name[0]) << 21) ^ (uint32(name[1]) << 14) ^ (uint32(name[2]) << 7) ^ (uint32(name[3]) << 0) ^ rol32(hash, 7*4)
			name = name[4:]
		}
	}

}

type dir2HashTable []Dir2LeafEntry

func (ht dir2HashTable) Len() int {
	return len(ht)
}

func (ht dir2HashTable) Less(i, j int) bool {
	return ht[i].HashVal < ht[j].HashVal
}

func (ht dir2HashTable) Swap(i, j int) {
	x := ht[i]
	ht[i] = ht[j]
	ht[j] = x
}

type dentry struct {
	Inode uint64
	Name  string
	FType uint8
}

func addDentry(w io.Writer, offset int64, dentry *dentry) int64 {

	l := 11 + int64(len(dentry.Name))
	pad := align(l, 8) - l

	err := binary.Write(w, binary.BigEndian, dentry.Inode)
	if err != nil {
		panic(err)
	}

	err = binary.Write(w, binary.BigEndian, uint8(len(dentry.Name)))
	if err != nil {
		panic(err)
	}

	_, err = io.Copy(w, strings.NewReader(dentry.Name))
	if err != nil {
		panic(err)
	}

	// _ = binary.Write(w, binary.BigEndian, dentry.FType) NOTE: this is for later versions of directories

	_, err = io.CopyN(w, vio.Zeroes, pad)
	if err != nil {
		panic(err)
	}

	err = binary.Write(w, binary.BigEndian, uint16(offset))
	if err != nil {
		panic(err)
	}

	return l + pad

}

func writeDir2Dentries(w io.Writer, header *Dir2Header, dentries []*dentry, offset, space, blockSize int64) (dir2HashTable, uint16) {

	hashTable := make(dir2HashTable, 0, len(dentries))

	for _, dentry := range dentries {

		hashTable = append(hashTable, Dir2LeafEntry{
			HashVal: hashname(dentry.Name),
			Address: uint32(offset / 8),
		})

		delta := addDentry(w, offset%blockSize, dentry)
		offset += delta
		space -= delta
	}

	return hashTable, processDir2BlockFreeSpace(w, header, offset, space, blockSize)

}

type blockDirBuilder struct {
	n *vio.TreeNode
	c *compiler

	entries   int64
	hashTable dir2HashTable
}

func writeDir2Data(w io.Writer, magic uint32, dentries []*dentry, offset, space, blockSize int64) (dir2HashTable, uint16) {

	var best uint16
	var hashTable dir2HashTable
	buf := new(bytes.Buffer)
	header := Dir2Header{
		Magic: magic,
	}
	hashTable, best = writeDir2Dentries(buf, &header, dentries, offset, space, blockSize)

	err := binary.Write(w, binary.BigEndian, &header)
	if err != nil {
		panic(err)
	}

	_, err = io.Copy(w, bytes.NewReader(buf.Bytes()))
	if err != nil {
		panic(err)
	}

	return hashTable, best

}

func (b *blockDirBuilder) process() {

	b.entries = 2 + int64(len(b.n.Children))

}

func (b *blockDirBuilder) writeData(w io.Writer) {

	dentries := []*dentry{}

	dentries = append(dentries, &dentry{
		Inode: b.c.inodeNumberFromNode(b.n),
		Name:  ".",
		FType: FTypeDirectory,
	})

	dentries = append(dentries, &dentry{
		Inode: b.c.inodeNumberFromNode(b.n.Parent),
		Name:  "..",
		FType: FTypeDirectory,
	})

	for _, child := range b.n.Children {

		ftype := uint8(FTypeRegularFile)
		if child.File.IsDir() {
			ftype = FTypeDirectory
		} else if child.File.IsSymlink() {
			ftype = FTypeSymlink
		}

		dentries = append(dentries, &dentry{
			Inode: b.c.inodeNumberFromNode(child),
			Name:  child.File.Name(),
			FType: ftype,
		})

	}

	offset := int64(16)
	space := b.c.blockSize()
	space -= offset
	space -= 8 * b.entries // hashtable
	space -= 8             // tail

	b.hashTable, _ = writeDir2Data(w, Dir2BlockMagic, dentries, offset, space, b.c.blockSize())

}

func (b *blockDirBuilder) writeHashTable(w io.Writer) {

	sort.Sort(b.hashTable)

	err := binary.Write(w, binary.BigEndian, b.hashTable)
	if err != nil {
		panic(err)
	}

}

func (b *blockDirBuilder) writeTail(w io.Writer) {

	tail := &Dir2BlockTail{
		Count: uint32(b.entries),
		Stale: 0,
	}

	err := binary.Write(w, binary.BigEndian, tail)
	if err != nil {
		panic(err)
	}

}

func (b *blockDirBuilder) generate() []byte {

	b.process()

	buf := new(bytes.Buffer)

	b.writeData(buf)
	b.writeHashTable(buf)
	b.writeTail(buf)

	return buf.Bytes()

}

func (c *compiler) calculateLengthOfBlockFormDirectoryData(n *vio.TreeNode) int64 {

	var size int64

	nblocks := int64(c.nodeBlocks[n.NodeSequenceNumber])
	size = c.blockSize() * nblocks

	return size

}

func (c *compiler) generateBlockFormDirectoryData(n *vio.TreeNode) io.Reader {

	_ = c.computeNodeExtents(n.NodeSequenceNumber, &dataRange{blocks: 1}) // called here to ensure things are computed in order

	b := &blockDirBuilder{
		n: n,
		c: c,
	}

	data := b.generate()

	return bytes.NewReader(data)

}

type leafDirBuilder struct {
	n       *vio.TreeNode
	c       *compiler
	blocks  int64
	extents []*extent

	entries      int64
	dataExtents  int64
	dataBlocks   int64
	bests        []uint16
	hashTable    dir2HashTable
	blockEntries [][]*dentry
}

func (b *leafDirBuilder) process() {

	b.entries = 2 + int64(len(b.n.Children))

	for _, e := range b.extents {
		if e.offset < 0x800000000/b.c.blockSize() {
			b.dataExtents++
		}
	}

	for i := int64(0); i < b.dataExtents; i++ {
		b.dataBlocks += b.extents[i].length
	}

	b.bests = make([]uint16, b.dataBlocks)
	b.blockEntries = make([][]*dentry, b.dataBlocks)
	block := 0
	space := b.c.blockSize() - 16

	addEntry := func(inode uint64, name string, ftype uint8) {

		l := align(int64(11+len(name)), 8)
		if space-l < 16 { // TODO: Really? Why 16? Why not zero?
			space = b.c.blockSize() - 16
			block++
		}
		space -= l

		b.blockEntries[block] = append(b.blockEntries[block], &dentry{
			Inode: inode,
			Name:  name,
			FType: ftype,
		})

	}

	addEntry(b.c.inodeNumberFromNode(b.n), ".", FTypeDirectory)
	addEntry(b.c.inodeNumberFromNode(b.n.Parent), "..", FTypeDirectory)

	for _, child := range b.n.Children {
		ftype := uint8(FTypeRegularFile)
		if child.File.IsDir() {
			ftype = FTypeDirectory
		} else if child.File.IsSymlink() {
			ftype = FTypeSymlink
		}
		addEntry(b.c.inodeNumberFromNode(child), child.File.Name(), ftype)
	}

}

func (b *leafDirBuilder) writeDataBlock(w io.Writer, n int64, dentries []*dentry) {

	space := b.c.blockSize() - 16
	offset := 16 + n*b.c.blockSize()

	hashes, best := writeDir2Data(w, Dir2BlockData, dentries, offset, space, b.c.blockSize())
	b.hashTable = append(b.hashTable, hashes...)
	b.bests[n] = best

}

func (b *leafDirBuilder) writeDataBlocks(w io.Writer) {

	for i := int64(0); i < b.dataBlocks; i++ {
		b.writeDataBlock(w, i, b.blockEntries[i])
	}

}

func (b *leafDirBuilder) writeLeafBlock(w io.Writer) {

	leafHeader := new(Dir2LeafHeader)
	leafHeader.Info.Forw = 0
	leafHeader.Info.Back = 0
	leafHeader.Info.Magic = Dir2Leaf1Magic
	leafHeader.Count = uint16(b.entries)
	leafHeader.Stale = 0

	sort.Sort(b.hashTable)

	tail := &Dir2LeafTail{
		BestCount: uint32(b.dataBlocks),
	}

	err := binary.Write(w, binary.BigEndian, leafHeader)
	if err != nil {
		panic(err)
	}

	err = binary.Write(w, binary.BigEndian, b.hashTable)
	if err != nil {
		panic(err)
	}

	padSize := b.c.blockSize()
	padSize -= 16                      // leaf header
	padSize -= int64(b.entries * 8)    // hash table
	padSize -= int64(2 * b.dataBlocks) // bests
	padSize -= 4                       // tail

	_, err = io.CopyN(w, vio.Zeroes, padSize)
	if err != nil {
		panic(err)
	}

	err = binary.Write(w, binary.BigEndian, b.bests)
	if err != nil {
		panic(err)
	}

	err = binary.Write(w, binary.BigEndian, tail)
	if err != nil {
		panic(err)
	}

}

func (b *leafDirBuilder) generate() []byte {

	b.process()

	buf := new(bytes.Buffer)

	b.writeDataBlocks(buf)
	b.writeLeafBlock(buf)

	return buf.Bytes()

}

func (c *compiler) calculateLengthOfLeafFormDirectoryData(n *vio.TreeNode) int64 {

	var size int64

	nblocks := int64(c.nodeBlocks[n.NodeSequenceNumber])
	nblocks--
	size = c.blockSize() * nblocks

	return size

}

func (c *compiler) generateLeafFormDirectoryData(n *vio.TreeNode, blocks int64, extents []*extent) io.Reader {

	b := &leafDirBuilder{
		n:       n,
		c:       c,
		blocks:  blocks,
		extents: extents,
	}

	data := b.generate()

	return bytes.NewReader(data)

}

type nodeDirBuilder struct {
	n       *vio.TreeNode
	c       *compiler
	blocks  int64
	extents []*extent

	entries      int64
	dataExtents  int64
	dataBlocks   int64
	leafBlocks   int64
	bests        []uint16
	hashTable    dir2HashTable
	blockEntries [][]*dentry
}

func (b *nodeDirBuilder) process() {

	b.entries = 2 + int64(len(b.n.Children))

	for _, e := range b.extents {
		if e.offset < 0x800000000/b.c.blockSize() {
			b.dataExtents++
		} else if e.offset >= 0x800000000/b.c.blockSize() && e.offset < 2*0x800000000/b.c.blockSize() {
			b.leafBlocks += e.length
		}
	}

	for i := int64(0); i < b.dataExtents; i++ {
		b.dataBlocks += b.extents[i].length
	}

	b.leafBlocks -= 1

	b.bests = make([]uint16, b.dataBlocks)
	b.blockEntries = make([][]*dentry, b.dataBlocks)
	block := 0
	space := b.c.blockSize() - 16

	addEntry := func(inode uint64, name string, ftype uint8) {

		l := align(int64(11+len(name)), 8)
		if space-l < 16 { // TODO: Really? Why 16? Why not zero?
			space = b.c.blockSize() - 16
			block++
		}
		space -= l

		b.blockEntries[block] = append(b.blockEntries[block], &dentry{
			Inode: inode,
			Name:  name,
			FType: ftype,
		})

	}

	addEntry(b.c.inodeNumberFromNode(b.n), ".", FTypeDirectory)
	addEntry(b.c.inodeNumberFromNode(b.n.Parent), "..", FTypeDirectory)

	for _, child := range b.n.Children {
		ftype := uint8(FTypeRegularFile)
		if child.File.IsDir() {
			ftype = FTypeDirectory
		} else if child.File.IsSymlink() {
			ftype = FTypeSymlink
		}
		addEntry(b.c.inodeNumberFromNode(child), child.File.Name(), ftype)
	}

}

func (b *nodeDirBuilder) writeDataBlock(w io.Writer, n int64, dentries []*dentry) {

	space := b.c.blockSize() - 16
	offset := 16 + n*b.c.blockSize()

	hashes, best := writeDir2Data(w, Dir2BlockData, dentries, offset, space, b.c.blockSize())
	b.hashTable = append(b.hashTable, hashes...)
	b.bests[n] = best

}

func (b *nodeDirBuilder) writeDataBlocks(w io.Writer) {

	for i := int64(0); i < b.dataBlocks; i++ {
		b.writeDataBlock(w, i, b.blockEntries[i])
	}

}

func (b *nodeDirBuilder) writeNodeBlock(w io.Writer) {

	nodeHeader := &Dir2NodeBlockHeader{
		Info: BlockInfo{
			Magic: Dir2NodeMagic,
		},
		Count: uint16(b.leafBlocks),
		Level: 1,
	}

	err := binary.Write(w, binary.BigEndian, nodeHeader)
	if err != nil {
		panic(err)
	}

	epb := int64((b.c.directoryBlockSize() - 16) / 8) // entries per block
	for i := int64(0); i < b.leafBlocks; i++ {
		blockNo := 0x800000000 / b.c.blockSize()
		blockNo += 1
		hv := uint32(0)
		if epb*(i+1) < int64(len(b.hashTable)) {
			hv = b.hashTable[epb*(i+1)-1].HashVal
		} else {
			hv = b.hashTable[len(b.hashTable)-1].HashVal
		}

		err = binary.Write(w, binary.BigEndian, uint32(hv)) // hashval
		if err != nil {
			panic(err)
		}

		err = binary.Write(w, binary.BigEndian, uint32(blockNo+i)) // before
		if err != nil {
			panic(err)
		}
	}

	_, err = io.CopyN(w, vio.Zeroes, b.c.directoryBlockSize()-16-8*b.leafBlocks) // padding
	if err != nil {
		panic(err)
	}

}

func (b *nodeDirBuilder) writeLeafBlock(w io.Writer, i int64) {

	nodeHeader := &Dir2NodeBlockHeader{
		Info: BlockInfo{
			Magic: Dir2LeafNMagic,
		},
		Count: 0,
	}

	blockNo := int64(0x800000000 / b.c.blockSize())
	blockNo += 1 + i

	if i > 0 {
		nodeHeader.Info.Back = uint32(blockNo - 1)
	}
	entriesPerBlock := (b.c.directoryBlockSize() - 16) / 8
	nodeHeader.Count = uint16(entriesPerBlock)
	if i < b.leafBlocks-1 {
		nodeHeader.Info.Forw = uint32(blockNo + 1)
	} else {
		if int64(len(b.hashTable))%entriesPerBlock != 0 {
			nodeHeader.Count = uint16(int64(len(b.hashTable)) % entriesPerBlock)
		}
	}
	slice := b.hashTable[i*entriesPerBlock:]
	slice = slice[:nodeHeader.Count]

	err := binary.Write(w, binary.BigEndian, nodeHeader)
	if err != nil {
		panic(err)
	}

	err = binary.Write(w, binary.BigEndian, slice)
	if err != nil {
		panic(err)
	}

	_, err = io.CopyN(w, vio.Zeroes, 8*(entriesPerBlock-int64(nodeHeader.Count)))
	if err != nil {
		panic(err)
	}

}

func (b *nodeDirBuilder) writeLeafBlocks(w io.Writer) {

	sort.Sort(b.hashTable)

	for i := int64(0); i < b.leafBlocks; i++ {
		b.writeLeafBlock(w, i)
	}

}

func (b *nodeDirBuilder) writeFreeIndexBlock(w io.Writer) {

	// free index
	freeIndexHeader := &Dir2FreeIndexHeader{
		Magic:   Dir2FreeMagic,
		FirstDB: 0,
		NUsed:   int32(len(b.bests)),
		NValid:  int32(len(b.bests)),
	}

	err := binary.Write(w, binary.BigEndian, freeIndexHeader)
	if err != nil {
		panic(err)
	}

	// _, _ = io.CopyN(buf, zeroes, c.directoryBlockSize()-16-2*int64(len(bests)))

	err = binary.Write(w, binary.BigEndian, b.bests)
	if err != nil {
		panic(err)
	}

	_, err = io.CopyN(w, vio.Zeroes, b.c.directoryBlockSize()-16-2*int64(len(b.bests)))
	if err != nil {
		panic(err)
	}

}

func (b *nodeDirBuilder) generate() []byte {

	b.process()

	buf := new(bytes.Buffer)

	b.writeDataBlocks(buf)
	b.writeNodeBlock(buf)
	b.writeLeafBlocks(buf)
	b.writeFreeIndexBlock(buf)

	return buf.Bytes()

}

func (c *compiler) generateNodeFormDirectoryData(n *vio.TreeNode, blocks int64, extents []*extent) io.Reader {

	b := &nodeDirBuilder{
		n:       n,
		c:       c,
		blocks:  blocks,
		extents: extents,
	}

	data := b.generate()

	return bytes.NewReader(data)

}

func (c *compiler) generateDirectory(n *vio.TreeNode) (size int64, data []byte, extents []*extent) {

	extents = make([]*extent, 0)
	nblocks := int64(c.nodeBlocks[n.NodeSequenceNumber])

	if nblocks == 0 {
		// short form
		data = generateShortFormDirectoryData(c, n)
		size = int64(len(data))
		return
	} else if nblocks == 1 {
		// block form
		size = c.calculateLengthOfBlockFormDirectoryData(n)
		extents = c.computeNodeExtents(n.NodeSequenceNumber, &dataRange{blocks: nblocks})
		return
	}

	// check if leaf format
	next := nblocks - 1
	entries := int64(2 + len(n.Children))
	lfll := 16 + 4 + 2*next + (8 * entries)
	if lfll <= c.directoryBlockSize() {
		// leaf format
		size = c.calculateLengthOfLeafFormDirectoryData(n)
		extents = c.computeNodeExtents(n.NodeSequenceNumber, &dataRange{blocks: nblocks - 1, offset: 0}, &dataRange{blocks: 1, offset: 0x800000000 / c.blockSize()})
		return
	}

	// node format
	ll := int64(16)

	grow := func(child string) {
		l := int64(len(child))
		l = 11 + l
		l = align(l, 8) // round up to nearest 8

		// TODO: shuffle entries to optimize used space

		delta := align(ll, c.blockSize()) - ll
		if delta < l || delta-l < 16 {
			ll += delta + 16
		}

		ll += l
	}

	grow(".")
	grow("..")

	for _, child := range n.Children {
		grow(child.File.Name())
	}

	ddl := ll // directory data length
	ddb := divide(ddl, c.directoryBlockSize())

	leafBlocksBytes := 16 + (8 * entries)
	headerSize := int64(16)
	leafBlocks := divide(leafBlocksBytes, c.directoryBlockSize()-headerSize)
	leafBlocks += 1

	freeIndexBytes := 16 + 2*next
	freeIndexBlocks := divide(freeIndexBytes, c.directoryBlockSize())

	size = ddb * c.blockSize()
	extents = c.computeNodeExtents(n.NodeSequenceNumber, &dataRange{blocks: ddb, offset: 0}, &dataRange{blocks: leafBlocks, offset: 0x800000000 / c.blockSize()}, &dataRange{blocks: freeIndexBlocks, offset: 2 * 0x800000000 / c.blockSize()})
	return
}

func (c *compiler) generateDirectoryBlockData(n *vio.TreeNode, blocks int64) io.Reader {
	if blocks == 1 {
		return c.generateBlockFormDirectoryData(n)
	}

	next := blocks - 1
	entries := int64(2 + len(n.Children))
	lfll := 16 + 4 + 2*next + (8 * entries)
	if lfll <= c.directoryBlockSize() {
		// leaf format
		extents := c.computeNodeExtents(n.NodeSequenceNumber, &dataRange{blocks: blocks - 1, offset: 0}, &dataRange{blocks: 1, offset: 0x800000000 / c.blockSize()}) // called here to ensure things are computed in order
		return c.generateLeafFormDirectoryData(n, blocks, extents)
	}

	// node format
	ll := int64(16)

	grow := func(child string) {
		l := int64(len(child))
		l = 11 + l
		l = align(l, 8) // round up to nearest 8

		// TODO: shuffle entries to optimize used space

		delta := align(ll, c.blockSize()) - ll
		if delta < l || delta-l < 16 {
			ll += delta + 16
		}

		ll += l
	}

	grow(".")
	grow("..")

	for _, child := range n.Children {
		grow(child.File.Name())
	}

	ddl := ll // directory data length
	ddb := divide(ddl, c.directoryBlockSize())

	leafBlocksBytes := 16 + (8 * entries)
	headerSize := int64(16)
	leafBlocks := divide(leafBlocksBytes, c.directoryBlockSize()-headerSize)
	leafBlocks += 1

	freeIndexBytes := 16 + 2*next
	freeIndexBlocks := divide(freeIndexBytes, c.directoryBlockSize())

	extents := c.computeNodeExtents(n.NodeSequenceNumber, &dataRange{blocks: ddb, offset: 0}, &dataRange{blocks: leafBlocks, offset: 0x800000000 / c.blockSize()}, &dataRange{blocks: freeIndexBlocks, offset: 2 * 0x800000000 / c.blockSize()})
	return c.generateNodeFormDirectoryData(n, blocks, extents)
}
