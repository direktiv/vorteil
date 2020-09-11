package vhd

type footer struct { // 512 bytes
	Cookie             uint64
	Features           uint32
	FileFormatVersion  uint32
	DataOffset         uint64
	TimeStamp          uint32
	CreatorApplication uint32
	CreatorVersion     uint32
	CreatorHostOS      uint32
	OriginalSize       uint64
	CurrentSize        uint64
	DiskGeometry       uint32
	DiskType           uint32
	Checksum           uint32
	UniqueID           [16]byte
	SavedState         byte
	Reserved           [427]byte
}

type header struct { // 1024 bytes
	Cookie              uint64
	DataOffset          uint64
	TableOffset         uint64
	HeaderVersion       uint32
	MaxTableEntries     uint32
	BlockSize           uint32
	Checksum            uint32
	ParentUniqueID      [16]byte
	ParentTimeStamp     uint32
	Reserved            [4]byte
	ParentUnicodeName   [512]byte
	ParentLocatorEntry1 [24]byte
	ParentLocatorEntry2 [24]byte
	ParentLocatorEntry3 [24]byte
	ParentLocatorEntry4 [24]byte
	ParentLocatorEntry5 [24]byte
	ParentLocatorEntry6 [24]byte
	ParentLocatorEntry7 [24]byte
	ParentLocatorEntry8 [24]byte
	Reserved2           [256]byte
}
