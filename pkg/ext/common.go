package ext

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
