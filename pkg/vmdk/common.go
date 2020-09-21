package vmdk

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

const (
	Magic           = 0x564d444b
	SectorSize      = 0x200
	GrainSize       = 0x10000
	SectorsPerGrain = GrainSize / SectorSize
	TableMaxRows    = 512
	TableRowSize    = 4
	TableSectors    = TableMaxRows * TableRowSize / SectorSize
)

type Header struct {
	MagicNumber        uint32 // 0
	Version            uint32 // 4
	Flags              uint32 // 8
	Capacity           uint64 // 12
	GrainSize          uint64 // 20
	DescriptorOffset   uint64 // 28
	DescriptorSize     uint64 // 36
	NumGTEsPerGT       uint32 // 44
	RGDOffset          uint64 // 48
	GDOffset           uint64 // 56
	OverHead           uint64 // 64
	UncleanShutdown    byte   // 72
	SingleEndLineChar  byte   // 73
	NonEndLineChar     byte   // 74
	DoubleEndLineChar1 byte   // 75
	DoubleEndLineChar2 byte   // 76
	CompressAlgorithm  uint16 // 77
	Pad                [433]uint8
}

func generateDiskUID() string {
	rand.Seed(time.Now().UTC().UnixNano())
	b := [4]byte{}
	binary.LittleEndian.PutUint32(b[:], uint32(rand.Int31()))
	return strings.ToUpper(fmt.Sprintf("%X", b))
}
