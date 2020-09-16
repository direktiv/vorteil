package vmdk

import (
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

type SparseWriter struct {
	w io.WriteSeeker
	h HolePredictor

	totalDataSectors int64
	totalDataGrains  int64
	totalTables      int64
	totalGDSectors   int64
	totalGTSectors   int64

	hdr          *Header
	cursor       int64
	grainOffsets []int64
}

func sparseDescriptor(name string, totalDataGrains int64) string {

	template := `# Disk DescriptorFile
version=1
CID=%s
parentCID=ffffffff
createType="monolithicSparse"

# Extent description
RW %d SPARSE "%s.vmdk"

# The Disk Data Base
#DDB

ddb.virtualHWVersion = "10"
ddb.adapterType = "ide"
`

	uid := generateDiskUID()
	description := fmt.Sprintf(template, uid, totalDataGrains*SectorsPerGrain, name)
	return description
}

func (w *SparseWriter) writeSparseHeader() error {

	hdr := new(Header)
	hdr.MagicNumber = Magic
	hdr.Version = 1
	hdr.Flags = 0x3
	hdr.GrainSize = SectorsPerGrain
	hdr.DescriptorOffset = 1
	hdr.DescriptorSize = 20
	hdr.NumGTEsPerGT = TableMaxRows
	hdr.RGDOffset = 21
	hdr.SingleEndLineChar = '\n'
	hdr.NonEndLineChar = ' '
	hdr.DoubleEndLineChar1 = '\r'
	hdr.DoubleEndLineChar2 = '\n'
	hdr.CompressAlgorithm = 0

	w.totalTables = (w.totalDataGrains + TableMaxRows - 1) / TableMaxRows

	w.totalGDSectors = (w.totalTables*4 + SectorSize - 1) / SectorSize
	w.totalGTSectors = w.totalTables * TableSectors

	// GDOffset comes after the redundant grain directory
	// and its grain tables.
	hdr.GDOffset = hdr.RGDOffset + uint64(w.totalGDSectors+w.totalGTSectors)

	// Overhead is measured in grains, not sectors.
	// Includes everything before the start of the disk contents.
	// TODO: this seems like a bug...
	hdr.OverHead = (((uint64(2*(w.totalGDSectors+w.totalGTSectors)) + hdr.RGDOffset) + SectorsPerGrain - 1) / SectorsPerGrain) * SectorsPerGrain

	hdr.Capacity = uint64(w.totalDataSectors)

	w.hdr = hdr

	err := binary.Write(w.w, binary.LittleEndian, w.hdr)
	if err != nil {
		return err
	}

	return nil

}

func (w *SparseWriter) writeGrainData() error {
	var err error

	firstDataSector := int64(w.hdr.OverHead) * SectorsPerGrain

	// rgd
	firstDirSector := int64(w.hdr.RGDOffset)
	firstTableSector := firstDirSector + w.totalGDSectors
	offset := firstDirSector * SectorSize
	_, err = w.w.Seek(offset, io.SeekStart)
	if err != nil {
		return err
	}

	for i := int64(0); i < w.totalTables; i++ {
		tableSector := int64(0)
		if i < w.totalTables {
			tableSector = firstTableSector + i*4
		}
		err := binary.Write(w.w, binary.LittleEndian, uint32(tableSector))
		if err != nil {
			return err
		}
	}

	// rgt
	offset = firstTableSector * SectorSize
	_, err = w.w.Seek(offset, io.SeekStart)
	if err != nil {
		return err
	}

	for i := int64(0); i < w.totalDataGrains; i++ {

		grainSector := int64(w.grainOffsets[i])
		if i+1 < int64(len(w.grainOffsets)) && grainSector == int64(w.grainOffsets[i+1]) {
			grainSector = 0
		}
		grainSector /= SectorSize

		if i%TableMaxRows == 0 {
			table := i / TableMaxRows
			offset = firstTableSector*SectorSize + TableSectors*table
		}

		err := binary.Write(w.w, binary.LittleEndian, uint32(grainSector))
		if err != nil {
			return err
		}
	}

	// gd
	firstDirSector = int64(w.hdr.GDOffset)
	firstTableSector = firstDirSector + w.totalGDSectors
	offset = firstDirSector * SectorSize
	_, err = w.w.Seek(offset, io.SeekStart)
	if err != nil {
		return err
	}

	for i := int64(0); i < w.totalTables; i++ {
		tableSector := int64(0)
		if i < w.totalTables {
			tableSector = firstTableSector + i*4
		}
		err := binary.Write(w.w, binary.LittleEndian, uint32(tableSector))
		if err != nil {
			return err
		}
	}

	// gt
	offset = firstTableSector * SectorSize
	_, err = w.w.Seek(offset, io.SeekStart)
	if err != nil {
		return err
	}

	for i := int64(0); i < w.totalDataGrains; i++ {

		grainSector := int64(w.grainOffsets[i])
		if i+1 < int64(len(w.grainOffsets)) && grainSector == int64(w.grainOffsets[i+1]) {
			grainSector = 0
		}
		grainSector /= SectorSize

		if i%TableMaxRows == 0 {
			table := i / TableMaxRows
			offset = firstTableSector*SectorSize + TableSectors*table
		}

		err := binary.Write(w.w, binary.LittleEndian, uint32(grainSector))
		if err != nil {
			return err
		}
	}

	// pad
	offset = firstDataSector * SectorSize
	_, err = w.w.Seek(firstDataSector*SectorSize, io.SeekStart)
	if err != nil {
		return err
	}

	return nil
}

func (w *SparseWriter) init() error {
	w.totalDataSectors = (w.h.Size() + SectorSize - 1) / SectorSize
	w.totalDataGrains = (w.totalDataSectors + SectorsPerGrain - 1) / SectorsPerGrain

	// write header
	err := w.writeSparseHeader()
	if err != nil {
		return err
	}

	// write descriptor
	name := "disk"
	description := sparseDescriptor(name, w.totalDataGrains)
	_, err = io.Copy(w.w, strings.NewReader(description))
	if err != nil {
		return err
	}

	firstDataSector := int64(w.hdr.OverHead) * SectorsPerGrain
	w.grainOffsets = make([]int64, w.totalDataGrains, w.totalDataGrains)
	offset := firstDataSector * SectorSize
	for i := int64(0); i < w.totalDataGrains; i++ {
		w.grainOffsets[i] = offset
		if w.h.RegionIsHole(i*GrainSize, GrainSize) {
			continue
		}
		offset += GrainSize
	}

	// grain directories & tables
	err = w.writeGrainData()
	if err != nil {
		return err
	}

	return nil
}

func (w *SparseWriter) Write(p []byte) (int, error) {

	k, err := w.w.Write(p)
	w.cursor += int64(k)
	return k, err

	// TODO: check not writing into a forbidden grain
}

func (w *SparseWriter) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = w.cursor + offset
	case io.SeekEnd:
		abs = w.h.Size() + offset
	default:
		panic("bad seek whence")
	}

	grain := abs / GrainSize
	delta := abs % GrainSize
	x := w.grainOffsets[grain] + delta
	_, err := w.w.Seek(x, io.SeekStart)
	w.cursor = abs
	if err != nil {
		return 0, err
	}
	return abs, nil

}

func (w *SparseWriter) Close() error {
	return nil
}

func NewSparseWriter(w io.WriteSeeker, h HolePredictor) (*SparseWriter, error) {

	x := &SparseWriter{
		w: w,
		h: h,
	}

	err := x.init()
	if err != nil {
		return nil, err
	}

	return x, nil

}
