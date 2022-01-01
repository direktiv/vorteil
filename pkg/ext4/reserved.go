package ext4

import (
	"bytes"
	"encoding/binary"
	"io"
	"io/ioutil"

	"github.com/vorteil/vorteil/pkg/vio"
)

const (
	ResizeInode = 7
)

func (s *super) resizeData() []byte {

	descriptorBlocks := divide(s.totalGroupDescriptors, DescriptorsPerBlock)

	buf := new(bytes.Buffer)

	for i := int64(0); i < descriptorBlocks && i < BlockSize/4; i++ {
		addr := 1 + i
		x := addr
		if int64(len(s.descriptors)-1)/DescriptorsPerBlock >= i {
			x = 0
		}
		err := binary.Write(buf, binary.LittleEndian, uint32(x))
		if err != nil {
			panic(err)
		}
	}

	growToBlock(buf)

	return buf.Bytes()

}

func (c *Compiler) initResizeNode() error {
	c.inodeBlocks[ResizeInode] = node{
		node: &vio.TreeNode{
			File: vio.CustomFile(vio.CustomFileArgs{
				Size: int(BlockSize),
				ReadCloser: vio.LazyReadCloser(func() (io.Reader, error) {
					return bytes.NewReader(c.resizeData()), nil
				}, func() error {
					return nil
				}),
			}),
			NodeSequenceNumber: ResizeInode,
			Links:              1,
		},
		start:   -1,
		content: 1,
		fs:      1,
	}
	return nil
}

func (s *super) generateResizeInode(n *node, mapper contentMapper) *Inode {

	inode := &Inode{}

	inode.Permissions = InodeDefaultRegularFilePermissions
	inode.UID = SuperUID
	inode.GID = SuperGID
	inode.Links = 1
	inode.SizeLower = 0x40C000                                                    // size of the "hole" where the direct pointers and single indirect pointers cover.
	inode.SizeUpper = 0x1                                                         // size of the total space coverable by the double indirect pointers.
	inode.Sectors = uint32((s.reservedGDTBlocksPerTable() + 1) * SectorsPerBlock) // redo

	var pointers [14]uint32
	addr, _ := mapper.mapContent(int64(n.start))
	pointers[13] = uint32(addr)
	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.LittleEndian, &pointers)
	if err != nil {
		panic(err)
	}

	copy(inode.Block[:], buf.Bytes())

	return inode

}

const (
	MinJournalBlocks = 1024
	MaxJournalBlocks = 32768
)

const (
	JBD2FeatureIncompatRevoke      = 0x1
	JBD2FeatureIncompat64Bit       = 0x2
	JBD2FeatureIncompatAsyncCommit = 0x4
)

type JournalBlockHeader struct {
	Magic     uint32
	Type      uint32
	Seq       uint32
	BlockSize uint32
	MaxLen    uint32
	First     uint32
	SuperSeq  uint32
	Start     uint32
	Errno     uint32

	// v2
	FeatureCompat   uint32
	FeatureInCompat uint32
	FeatureRoCompat uint32

	UUID            [16]byte
	NrUsers         uint32
	DynSuper        uint32
	MaxTransactions uint32
	MaxTransData    uint32
	ChecksumType    uint8
	Padding2        [3]byte
	Padding         [42]uint32
	Checksum        uint32
	Users           [16 * 48]byte
}

func journalData(blocks int64) []byte {

	jhdr := &JournalBlockHeader{
		Magic:           0xC03B3998, //0x98393BC0
		Type:            4,          // v1
		BlockSize:       BlockSize,
		MaxLen:          uint32(blocks),
		First:           1,
		Start:           0,
		FeatureInCompat: JBD2FeatureIncompatAsyncCommit | JBD2FeatureIncompatRevoke | JBD2FeatureIncompat64Bit,
	}

	buf := new(bytes.Buffer)

	err := binary.Write(buf, binary.BigEndian, jhdr)
	if err != nil {
		panic(err)
	}

	return buf.Bytes()

}

func (c *Compiler) initJournalNode(totalBlocks int64, limiters ...int64) error {

	journalBlocks := int64(MaxJournalBlocks)
	if totalBlocks/10 < journalBlocks {
		journalBlocks = totalBlocks / 10
	}
	for _, limit := range limiters {
		if limit < journalBlocks {
			journalBlocks = limit
		}
	}
	if journalBlocks < MinJournalBlocks {
		journalBlocks = MinJournalBlocks
		// mib := divide(MinJournalBlocks-journalBlocks, 1024)
		// fmt.Fprintf(os.Stderr, "totalBlocks %v, limiters %v, journalBlocks %v MinJournalBlocks %v\n", totalBlocks, limiters, journalBlocks, MinJournalBlocks)
		// return fmt.Errorf("not enough space to contain journal -- try making the disk size roughly %v MiB larger", mib)
	}

	c.inodeBlocks[JournalInode] = node{
		node: &vio.TreeNode{
			File: vio.CustomFile(vio.CustomFileArgs{
				Size:       int(journalBlocks * BlockSize),
				ReadCloser: ioutil.NopCloser(bytes.NewReader(journalData(journalBlocks))),
			}),
			NodeSequenceNumber: JournalInode,
			Links:              1,
		},
		start:   -(journalBlocks + 1), // +1 because of the resize inode indirect block
		content: uint32(journalBlocks),
		fs:      uint32(journalBlocks),
	}

	return nil

}
