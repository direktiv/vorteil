package ext4

import (
	"bytes"
	"encoding/binary"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/vorteil/vorteil/pkg/vio"
)

const (
	DirentHashVersion = 0x2
)

// FTYPE constants are used in directory entries to identify file types without requiring inode lookups.
const (
	FTypeRegularFile = 0x1 // FTYPE_REGULAR_FILE
	FTypeDir         = 0x2 // FTYPE_DIR
	FTypeSymlink     = 0x7 // FTYPE_SYMLINK
)

func sliceStringForHashing(s string) (string, *[4]uint32) {

	var pad, val uint32
	var in *[4]uint32
	in = &[4]uint32{}

	l := len(s)

	pad = uint32(l) | (uint32(l) << 8)
	pad |= pad << 16
	val = pad

	l = 16
	if len(s) < l {
		l = len(s)
	}

	var i, c int
	for i = 0; i < l; i++ {
		val = uint32(s[i]) + (val << 8)
		if (i % 4) == 3 {
			in[c] = val
			c++
			val = pad
		}
	}

	if c < 4 {
		in[c] = val
		c++
	}

	for c < 4 {
		in[c] = pad
		c++
	}

	return s[l:], in

}

func teaTransform(buf, p *[4]uint32) {

	var sum, b0, b1, a, b, c, d uint32
	b0 = buf[0]
	b1 = buf[1]
	a = p[0]
	b = p[1]
	c = p[2]
	d = p[3]

	for i := 0; i < 16; i++ {
		sum += 0x9E3779B9
		b0 += ((b1 << 4) + a) ^ (b1 + sum) ^ ((b1 >> 5) + b)
		b1 += ((b0 << 4) + c) ^ (b0 + sum) ^ ((b0 >> 5) + d)
	}

	buf[0] += b0
	buf[1] += b1

}

func teaHash(s string) uint32 {

	var buf [4]uint32
	var p *[4]uint32

	// This is the starting state of the hashing buffer. Don't ask why, that's just the way it is.
	buf[0] = 0x67452301
	buf[1] = 0xefcdab89
	buf[2] = 0x98badcfe
	buf[3] = 0x10325476

	for len(s) > 0 {
		s, p = sliceStringForHashing(s)
		teaTransform(&buf, p)
	}

	hash := buf[0]
	hash = hash &^ 0x1

	// cap hash to a maximum value
	cap := uint32(0xFFFFFFFC)
	if hash > cap {
		hash = cap
	}

	return hash

}

func dentryHash(s string) uint32 {
	return teaHash(s)
}

func dentryMinLength(s string) int64 {
	l := 8 + align(int64(len(s)+1), 4)
	return l
}

type dentry struct {
	Inode    uint32
	RecLen   uint16
	NameLen  uint8
	FileType uint8
	// name string
	// padding
}

func writeDentry(w io.Writer, name string, dentry *dentry) error {

	err := binary.Write(w, binary.LittleEndian, dentry)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, strings.NewReader(name))
	if err != nil {
		return err
	}

	l := int64(dentry.RecLen) - 8
	l -= int64(len(name))
	_, err = io.CopyN(w, vio.Zeroes, l)
	if err != nil {
		return err
	}

	return nil

}

func calculateLinearDirectorySize(n *vio.TreeNode) int64 {

	var length, leftover int64
	length = 24 // '.' + '..' entries
	leftover = BlockSize - length

	for i, child := range n.Children {

		l := dentryMinLength(child.File.Name())

		if leftover >= l && (leftover-l == 0 || leftover-l > 8) {
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

	length = align(length, BlockSize)
	return length

}

type dirTuple struct {
	name  string
	inode uint32
	ftype uint8
}

func addLinearDirectoryBlock(w io.Writer, tuples []*dirTuple) error {

	buf := new(bytes.Buffer)
	length := int64(0)
	leftover := int64(BlockSize)
	exceedsBlock := false

	for i, child := range tuples {

		if exceedsBlock {
			panic("addLinearDirectoryBlock tried to write more than a block worth")
		}

		l := dentryMinLength(child.name)

		length += l
		leftover -= l

		if leftover < 8 || i == len(tuples)-1 {
			l += leftover
			length += leftover
			leftover = int64(BlockSize)
			exceedsBlock = true
		}

		err := writeDentry(buf, child.name, &dentry{
			Inode:    child.inode,
			RecLen:   uint16(l),
			NameLen:  uint8(len(child.name)),
			FileType: child.ftype,
		})
		if err != nil {
			return err
		}

	}

	growToBlock(buf)

	_, err := io.Copy(w, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return err
	}

	return nil

}

func generateLinearDirectoryData(n *node) []byte {

	var tuples []*dirTuple
	tuples = append(tuples, &dirTuple{name: ".", inode: uint32(n.node.NodeSequenceNumber), ftype: FTypeDir})
	tuples = append(tuples, &dirTuple{name: "..", inode: uint32(n.node.Parent.NodeSequenceNumber), ftype: FTypeDir})

	for _, child := range n.node.Children {
		var ftype uint8
		if child.File.IsDir() {
			ftype = FTypeDir
		} else if child.File.IsSymlink() {
			ftype = FTypeSymlink
		} else {
			ftype = FTypeRegularFile
		}
		tuples = append(tuples, &dirTuple{name: path.Base(child.File.Name()), inode: uint32(child.NodeSequenceNumber), ftype: ftype})
	}

	buf := new(bytes.Buffer)

	begin := 0
	size := int64(0)
	for i, tuple := range tuples {
		l := dentryMinLength(tuple.name)
		size += l
		if size > BlockSize {
			err := addLinearDirectoryBlock(buf, tuples[begin:i])
			if err != nil {
				panic(err)
			}
			begin = i
			size = l
		}
	}

	err := addLinearDirectoryBlock(buf, tuples[begin:])
	if err != nil {
		panic(err)
	}

	return buf.Bytes()

}

type hashDirEntryMetadata struct {
	hash   uint32
	length uint32
	node   *vio.TreeNode
}

type hashDirEntriesMetdata []hashDirEntryMetadata

func (x hashDirEntriesMetdata) Len() int {
	return len(x)
}

func (x hashDirEntriesMetdata) Less(i, j int) bool {
	return x[i].hash < x[j].hash
}

func (x hashDirEntriesMetdata) Swap(i, j int) {
	tmp := x[i]
	x[i] = x[j]
	x[j] = tmp
}

func calculateHashDirectorySize(n *vio.TreeNode) int64 {

	entries := make(hashDirEntriesMetdata, len(n.Children))

	for i, child := range n.Children {
		entries[i].length = uint32(dentryMinLength(child.File.Name()))
		entries[i].hash = dentryHash(child.File.Name())
	}

	sort.Sort(entries)

	var blocks []hashDirEntriesMetdata

	first := 0
	l := uint32(0)

	for i := range entries {

		if l+entries[i].length > BlockSize {
			blocks = append(blocks, entries[first:i])
			first = i
			l = entries[i].length
			continue
		}

		l += entries[i].length

	}

	blocks = append(blocks, entries[first:])

	numDataBlocks := int64(len(blocks))
	numInnerBlocks := int64(1)

	// NOTE: directories would need to be huge for the number of inner blocks to be greater than 1, so we aren't handling that yet

	return BlockSize * (numDataBlocks + numInnerBlocks)

}

func calculateDirectoryBlocks(n *vio.TreeNode) int64 {

	size := calculateLinearDirectorySize(n)
	blocks := divide(size, BlockSize)
	if blocks >= 2 {
		size = calculateHashDirectorySize(n)
	}

	return calculateBlocksFromSize(size)

}

// HashDirectoryEntry is one entry in a hash table within an indexed directory.
type HashDirectoryEntry struct {
	Hash  uint32
	Block uint32
}

// HashDirectoryRoot is the struct containing the full layout of the zeroth block for any indexed directory.
type HashDirectoryRoot struct {
	DotInode       uint32                  // 0x0
	DotRecLen      uint16                  // 0x4
	DotNameLen     uint8                   // 0x6
	DotFType       uint8                   // 0x7
	DotName        [4]byte                 // 0x8
	DotDotInode    uint32                  // 0xC
	DotDotRecLen   uint16                  // 0x10
	DotDotNameLen  uint8                   // 0x12
	DotDotFType    uint8                   // 0x13
	DotDotName     [4]byte                 // 0x14
	_              uint32                  // 0x18
	HashVersion    uint8                   // 0x1C
	InfoLength     uint8                   // 0x1D
	IndirectLevels uint8                   // 0x1E
	_              uint8                   // 0x1F
	Limit          uint16                  // 0x20
	Count          uint16                  // 0x22
	Block          uint32                  // 0x24
	Entries        [507]HashDirectoryEntry // 0x28
}

func addBlockToBuffer(w io.Writer, block hashDirEntriesMetdata) error {

	var tuples []*dirTuple

	for _, child := range block {

		var ftype uint8
		if child.node.File.IsDir() {
			ftype = FTypeDir
		} else if child.node.File.IsSymlink() {
			ftype = FTypeSymlink
		} else {
			ftype = FTypeRegularFile
		}

		tuples = append(tuples, &dirTuple{
			name:  child.node.File.Name(),
			inode: uint32(child.node.NodeSequenceNumber),
			ftype: ftype,
		})

	}

	err := addLinearDirectoryBlock(w, tuples)
	if err != nil {
		return err
	}

	return nil

}

func generateHashDirectoryData(node *node) []byte {
	n := node.node
	entries := make(hashDirEntriesMetdata, len(n.Children))

	for i, child := range n.Children {
		entries[i].length = uint32(dentryMinLength(child.File.Name()))
		entries[i].hash = dentryHash(child.File.Name())
		entries[i].node = child
	}

	sort.Sort(entries)

	var blocks []hashDirEntriesMetdata

	first := 0
	l := uint32(0)

	for i := range entries {

		if l+entries[i].length > BlockSize {
			blocks = append(blocks, entries[first:i])
			first = i
			l = entries[i].length
			continue
		}

		l += entries[i].length

	}

	blocks = append(blocks, entries[first:])

	// TODO: make this capable of accepting large amounts of inner blocks

	buf := new(bytes.Buffer)

	root := &HashDirectoryRoot{
		DotInode:       uint32(n.NodeSequenceNumber),
		DotRecLen:      12,
		DotNameLen:     1,
		DotFType:       FTypeDir,
		DotName:        [4]byte{'.', 0, 0, 0},
		DotDotInode:    uint32(n.Parent.NodeSequenceNumber),
		DotDotRecLen:   BlockSize - 12,
		DotDotNameLen:  2,
		DotDotFType:    FTypeDir,
		DotDotName:     [4]byte{'.', '.', 0, 0},
		HashVersion:    DirentHashVersion,
		InfoLength:     8,
		IndirectLevels: 0, // TODO: support deeper trees
		Limit:          507 + 1,
		Count:          uint16(len(blocks)), // + 1,
		Block:          1,
	}

	// for i, block := range blocks {
	for i := 1; i < len(blocks); i++ {
		block := blocks[i]
		root.Entries[i-1].Block = uint32(i + 1)
		root.Entries[i-1].Hash = block[0].hash
	}

	err := binary.Write(buf, binary.LittleEndian, root)
	if err != nil {
		panic(err)
	}

	for _, block := range blocks {
		err = addBlockToBuffer(buf, block)
		if err != nil {
			panic(err)
		}
	}

	return buf.Bytes()

}

func generateDirectoryData(node *node) (io.Reader, error) {

	if node.fs == 0 {
		return bytes.NewReader([]byte{}), nil
	}

	if node.fs == 1 {
		return bytes.NewReader(generateLinearDirectoryData(node)), nil
	}

	return bytes.NewReader(generateHashDirectoryData(node)), nil

}
