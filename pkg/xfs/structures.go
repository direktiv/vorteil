package xfs

const (
	SBMagicNumber    = 0x58465342 // "XFSB"
	sectorSizeLog    = 9
	SectorSize       = 0x1 << sectorSizeLog
	inodesPercentage = 25

	dirBlockAllocLog = 0 // 1 block

	VersionNumber      = 4      // XFS_SB_VERSION_4
	VersionAttrBit     = 0x0010 // XFS_SB_VERSION_ATTRBIT
	VersionNlinkBit    = 0x0020 // XFS_SB_VERSION_NLINKBIT
	VersionQuotaBit    = 0x0040 // XFS_SB_VERSION_QUOTABIT
	VersionAlignBit    = 0x0080 // XFS_SB_VERSION_ALIGNBIT
	VersionDalignBit   = 0x0100 // XFS_SB_VERSION_DALIGNBIT
	VersionSharedBit   = 0x0200 // XFS_SB_VERSION_SHAREDBIT
	VersionLogV2Bit    = 0x0400 // XFS_SB_VERSION_LOGV2BIT
	VersionSectorBit   = 0x0800 // XFS_SB_VERSION_SECTORBIT
	VersionExtFlgBit   = 0x1000 // XFS_SB_VERSION_EXTFLGBIT
	VersionDirV2Bit    = 0x2000 // XFS_SB_VERSION_DIRV2BIT
	VersionBorgBit     = 0x4000 // XFS_SB_VERSION_BORGBIT
	VersionMoreBitsBit = 0x8000 // XFS_SB_VERSION_MOREBITSBIT

	Version2Reserved1Bit   = 0x00000001 // XFS_SB_VERSION2_RESERVED1BIT
	Version2LazySBCountBit = 0x00000002 // XFS_SB_VERSION2_LAZYSBCOUNTBIT
	Version2Reserved4Bit   = 0x00000004 // XFS_SB_VERSION2_RESERVED4BIT
	Version2Attr2Bit       = 0x00000008 // XFS_SB_VERSION2_ATTR2BIT
	Version2ParentBit      = 0x00000010 // XFS_SB_VERSION2_PARENTBIT
	Version2ProjID32Bit    = 0x00000080 // XFS_SB_VERSION2_PROJID32BIT
	Version2CRCBit         = 0x00000100 // XFS_SB_VERSION2_CRCBIT
	Version2Ftype          = 0x00000200 // XFS_SB_VERSION2_FTYPE

	AGFMagicNumber = 0x58414746 // "XAGF"
	AGFVersion     = 1          // XFS_AGF_VERSION

	AGIMagicNumber = 0x58414749 // "XAGI"
	AGIVersion     = 1          // XFS_AGI_VERSION

	ABTBMagicNumber = 0x41425442 // "ABTB"
	ABTCMagicNumber = 0x41425443 // "ABTC"
	IBTMagicNumber  = 0x49414254 // "IABT"

	Dir2DataFDCount = 3          // XFS_DIR2_DATA_FD_COUNT
	Dir2BlockMagic  = 0x58443242 // XFS_DIR2_BLOCK_MAGIC "XD2B"
	Dir2BlockData   = 0x58443244 // XFS_DIR2_DATA_MAGIC "XD2D"
	Dir2Leaf1Magic  = 0xD2F1     // XFS_DIR2_LEAF1_MAGIC
	Dir2FreeMagic   = 0x58443246 // XFS_DIR2_FREE_MAGIC "XD2F"
	Dir2NodeMagic   = 0xFEBE     // XFS_DA_NODE_MAGIC
	Dir2LeafNMagic  = 0xD2FF     // XFS_DIR2_LEAFN_MAGIC

	FTypeRegularFile  = 1
	FTypeDirectory    = 2
	FTypeCharSpecial  = 3
	FTypeBlockSpecial = 4
	FTypeFIFO         = 5
	FTypeSocket       = 6
	FTypeSymlink      = 7

	XLogMagicNumber = 0xFEEDBABE

	InodeMagicNumber = 0x494e // "IN" (in ascii)

	InodeFormatDev     = 0
	InodeFormatLocal   = 1
	InodeFormatExtents = 2
	InodeFormatBTree   = 3
)

type SuperBlock struct {
	MagicNumber                     uint32   // 0
	BlockSize                       uint32   // 4
	DataBlocks                      uint64   // 8
	RealtimeBlocks                  uint64   // 16
	RealtimeExtents                 uint64   // 24
	UUID                            [16]byte // 32
	LogStart                        uint64   // 48
	RootInode                       uint64   // 56
	RealtimeBitmapInode             uint64   // 64
	RealtimeSummaryInode            uint64   // 72
	RealtimeExtentBlocks            uint32   // 80
	AGBlocks                        uint32   // 84
	AGCount                         uint32   // 88
	RealtimeBitmapBlocks            uint32   // 92
	LogBlocks                       uint32   // 96
	VersionNum                      uint16   // 100
	SectorSize                      uint16   // 102
	InodeSize                       uint16   // 104
	InodesPerBlock                  uint16   // 106
	FSName                          [12]byte // 108
	BlockSizeLogarithmic            uint8    // 120
	SectorSizeLogarithmic           uint8    // 121
	InodeSizeLogarithmic            uint8    // 122
	InodesPerBlockLogarithmic       uint8    // 123
	AGBlocksLogarithmic             uint8    // 124
	RealtimeExtentBlocksLogarithmic uint8    // 125
	InProgress                      uint8    // 126
	InodesMaxPercentage             uint8    // 127
	InodesAllocated                 uint64   // 128
	InodesFree                      uint64   // 136
	DataFree                        uint64   // 144
	RealtimeExtentsFree             uint64   // 152
	UserQuotasInode                 uint64   // 160
	GroupQuotasInode                uint64   // 168
	QuotaFlags                      uint16   // 176
	MiscFlags                       uint8    // 178
	SharedVN                        uint8    // 179
	InodeChunkAlignment             uint32   // 180 // TODO: WHAT?
	StripeUnitBlocks                uint32   // 184
	StripeWidthBlocks               uint32   // 188
	DirectoryBlocksLogarithmic      uint8    // 192
	LogSectorSizeLogarithmic        uint8    // 193
	LogSectorSize                   uint16   // 194
	LogStripeUnit                   uint32   // 196
	MoreFeatures                    uint32   // 200
	BadFeatures                     uint32   // 204

	// /* Version 5 only */
	// RWFeatureFlags       uint32   // 208
	// ROFeatureFlags       uint32   // 212
	// RWIncompatFlags      uint32   // 216
	// RWIncompatLogFlags   uint32   // 220
	// Checksum             uint32   // 224
	// SparseInodeAlignment uint32   // 228
	// ProjectQuotaInode    uint64   // 232
	// LastLoqSeqNo         uint64   // 240
	// UUID2                [16]byte // 248
	// RMBTInode            uint64   // 264
}

type AGF struct {
	Magic       uint32    // 0
	Version     uint32    // 4
	SeqNo       uint32    // 8
	Length      uint32    // 12
	Roots       [2]uint32 // 16
	Spare0      uint32    // 24
	Levels      [2]uint32 // 28
	Spare1      uint32    // 36
	FLFirst     uint32    // 40
	FLLast      uint32    // 44
	FLCount     uint32    // 48
	FreeBlocks  uint32    // 52
	Longest     uint32    // 56
	BTreeBlocks uint32    // 60
}

type AGI struct {
	Magic     uint32     // 0
	Version   uint32     // 4
	SeqNo     uint32     // 8
	Length    uint32     // 12
	Count     uint32     // 16
	Root      uint32     // 20
	Level     uint32     // 24
	FreeCount uint32     // 28
	NewIno    uint32     // 32
	DirIno    uint32     // 36
	Unlinked  [64]uint32 // 40
}

type BTreeSBlock struct {
	Magic    uint32 // 0
	Level    uint16 // 4
	NumRecs  uint16 // 6
	LeftSIB  uint32 // 8
	RightSIB uint32 // 12
}

type AllocRecord struct {
	StartBlock uint32 // 0
	BlockCount uint32 // 4
}

type InodeBTRecord struct {
	StartIno  uint32 // 0
	FreeCount uint32 // 4
	Free      uint64 // 8
}

type Dir2FreeEntry struct {
	Offset uint16 // 0
	Length uint16 // 2
}

type Dir2Header struct {
	Magic    uint32                         // 0
	BestFree [Dir2DataFDCount]Dir2FreeEntry // 4
} // 16

type Dir2LeafEntry struct {
	HashVal uint32 // 0
	Address uint32 // 4
} // 8

type Dir2UnusedEntry struct {
	FreeTag uint16 // 0
	Length  uint16 // 2
	Padding uint16 // 6
	Tag     uint16 // 4
}

type Dir2BlockTail struct {
	Count uint32 // 0
	Stale uint32 // 4
}

type Dir2LeafTail struct {
	BestCount uint32 // 0
} // 4

type BlockInfo struct {
	Forw  uint32
	Back  uint32
	Magic uint16
	Pad   uint16
}

type Dir2LeafHeader struct {
	Info  BlockInfo // 0
	Count uint16    // 12
	Stale uint16    // 14
} // 16

type XLogRecHeader struct {
	Magic     uint32     // 0
	Cycle     uint32     // 4
	Version   uint32     // 8
	Len       uint32     // 12
	LSN       uint64     // 16
	TailLSN   uint64     // 24
	CRC       uint32     // 32
	PrevBlock uint32     // 36
	NumLogOps uint32     // 40
	CycleData [64]uint32 // 44
	Fmt       uint32     // 300
	FSUUID    [16]byte   // 304
	Size      uint32     // 320
	Padding   [188]byte  // 324
}

type XLogRecord struct {
	TransactionID uint32 // 0
	Length        uint32 // 4
	ClientID      uint8  // 8
	Flags         uint8  // 9
	_             uint16 // 10
	Unknown       uint16 // 12
}

type Timestamp struct {
	Sec  uint32 // 0
	NSec uint32 // 4
}

type InodeCore struct {
	Magic        uint16    // 0
	Mode         uint16    // 2
	Version      uint8     // 4
	Format       uint8     // 5
	Onlink       uint16    // 6
	UID          uint32    // 8
	GID          uint32    // 12
	Nlink        uint32    // 16
	ProjID       uint16    // 20
	Pad          [8]byte   // 22
	FlushIter    uint16    // 30
	ATime        Timestamp // 32
	MTime        Timestamp // 40
	CTime        Timestamp // 48
	Size         int64     // 56
	NBlocks      uint64    // 64
	ExtSize      uint32    // 72
	NExtents     int32     // 76
	ANExtents    int16     // 80
	ForkOff      uint8     // 82
	AFormat      int8      // 83
	DMevMask     uint32    // 84
	DMState      uint16    // 88
	Flags        uint16    // 90
	Gen          uint32    // 92
	NextUnlinked uint32    // 96
} // 100

type Dir2FreeIndexHeader struct {
	Magic   uint32
	FirstDB int32
	NValid  int32
	NUsed   int32
}

type Dir2NodeBlockHeader struct {
	Info  BlockInfo
	Count uint16
	Level uint16
}
