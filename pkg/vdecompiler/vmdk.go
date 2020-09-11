package vdecompiler

import (
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"
)

type vmdkInfo struct {
	header *VMDKHeader
}

// Image file format.
const (
	ImageFormatVMDKSparse          = "vmdk (sparse extent)"
	ImageFormatVMDKStreamOptimized = "vmdk (stream optimized extent)"
	VMDKMagicNumber                = 0x564d444b
)

// VMDKHeaderFields keep all of the VMDK header information except for the magic number.
// The complete VMDK header is called VMDKHeader.
type VMDKHeaderFields struct {
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

// VMDKHeader keeps all of the VMDK header information.
type VMDKHeader struct {
	MagicNumber uint32           // 0
	Fields      VMDKHeaderFields // 4
}

type vmdkSparseIO struct {
	iio         *IO
	grain       int
	totalGrains int
	grainSize   int
	offset      int
	remainder   int
	grains      []uint32
	buffer      io.Reader
}

func (sio *vmdkSparseIO) loadGrain(grain int) (io.Reader, error) {
	if grain >= len(sio.grains) {
		panic(errors.New("grain out of bounds"))
	}

	offset := sio.grains[grain]
	_, err := sio.iio.src.Seek(int64(offset)*SectorSize, io.SeekStart)
	if err != nil {
		return nil, err
	}
	return io.LimitReader(sio.iio.src, int64(sio.grainSize)), nil
}

func (sio *vmdkSparseIO) Read(p []byte) (n int, err error) {

	if sio.remainder <= 0 {
		sio.grain++
		if sio.grain > sio.totalGrains {
			return 0, io.EOF
		}
		var data io.Reader
		data, err = sio.loadGrain(sio.grain)
		if err != nil {
			return
		}
		sio.buffer = data
		sio.remainder = sio.grainSize
		sio.offset = sio.grain * sio.grainSize
	}

	n, err = sio.buffer.Read(p)
	sio.remainder -= n
	sio.offset += n
	if err == io.EOF && sio.remainder == 0 {
		err = nil
	}
	return
}

func (sio *vmdkSparseIO) Seek(offset int64, whence int) (off int64, err error) {

	var x int64
	switch whence {
	case io.SeekStart:
		x = offset
	case io.SeekCurrent:
		x = offset + int64(sio.offset)
	case io.SeekEnd:
		x = offset + int64(sio.totalGrains)*int64(sio.grainSize)
	default:
		panic("unexpected 'whence' value")
	}

	if x >= int64(sio.totalGrains)*int64(sio.grainSize) {
		sio.remainder = 0
		sio.offset = int(sio.totalGrains) * int(sio.grainSize)
		return int64(sio.offset), nil
	}

	grain := x / int64(sio.grainSize)
	remainder := x % int64(sio.grainSize)

	sio.grain = int(grain)

	data, err := sio.loadGrain(int(grain))
	if err != nil {
		return int64(sio.offset), err
	}

	sio.buffer = data
	sio.remainder = sio.grainSize
	sio.offset = sio.grain * sio.grainSize

	_, err = io.CopyN(ioutil.Discard, sio, remainder)
	if err != nil {
		return int64(sio.offset), err
	}

	return int64(sio.offset), nil
}

func (sio *vmdkSparseIO) Write(p []byte) (n int, err error) {
	return 0, errors.New("writing not supported")
}

func (iio *IO) vmdkSparseIO() (*partialIO, error) {

	pio := new(partialIO)
	pio.name = iio.src.name // TODO: ?
	pio.size = int(iio.vmdk.header.Fields.Capacity) * SectorSize
	pio.closer = iio.src.closer

	sio := new(vmdkSparseIO)
	sio.grain = -1
	sio.iio = iio
	sio.grainSize = int(iio.vmdk.header.Fields.GrainSize) * SectorSize
	sio.totalGrains = pio.size / sio.grainSize
	pio.reader = sio
	pio.seeker = sio
	pio.writer = sio

	tables := (sio.totalGrains + 511) / 512
	gdes := make([]uint32, tables)

	_, err := iio.src.Seek(int64(iio.vmdk.header.Fields.GDOffset)*SectorSize, io.SeekStart)
	if err != nil {
		return nil, err
	}

	err = binary.Read(iio.src, binary.LittleEndian, &gdes)
	if err != nil {
		return nil, err
	}

	for i := 0; i < tables; i++ {
		_, err = iio.src.Seek(int64(gdes[i])*SectorSize, io.SeekStart)
		if err != nil {
			return nil, err
		}

		gtes := make([]uint32, 512)
		err = binary.Read(iio.src, binary.LittleEndian, &gtes)
		if err != nil {
			return nil, err
		}

		sio.grains = append(sio.grains, gtes...)
	}

	sio.grains = sio.grains[:sio.totalGrains]

	return pio, nil
}
