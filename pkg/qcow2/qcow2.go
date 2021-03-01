package qcow2

import (
	"bytes"
	"encoding/binary"
	"io"
)

const (
	SectorSize = 0x200
)

type HolePredictor interface {
	Size() int64
	RegionIsHole(begin, size int64) bool
}

type Writer struct {
	w io.WriteSeeker
	h HolePredictor

	cursor int64

	totalDataSectors      int64
	totalDataClusters     int64
	metadataClusters      int64
	clusterSize           int64
	sectorsPerCluster     int64
	clusterOffsets        []int64
	clusterInUse          []bool
	l1Size                int64
	l2Size                int64
	l2Blocks              int64
	l2Offset              int64
	refcountBlocks        int64
	refcountTableClusters int64
}

func NewWriter(w io.WriteSeeker, h HolePredictor) (*Writer, error) {

	x := &Writer{
		w: w,
		h: h,
	}

	err := x.init()
	if err != nil {
		return nil, err
	}

	return x, nil

}

func divide(x, y int64) int64 {
	return (x + y - 1) / y
}

func (w *Writer) init() error {

	w.clusterSize = 0x10000
	w.sectorsPerCluster = w.clusterSize / SectorSize

	w.totalDataSectors = divide(w.h.Size(), SectorSize)
	w.totalDataClusters = divide(w.totalDataSectors, w.sectorsPerCluster)

	w.l2Blocks = divide(w.totalDataClusters, w.clusterSize/8)
	w.l2Size = w.clusterSize
	w.l1Size = divide(w.l2Blocks, w.clusterSize/8)

	w.refcountBlocks = divide(w.totalDataClusters, w.clusterSize/2)
	w.refcountTableClusters = divide(w.refcountBlocks, w.clusterSize/8)

	// NOTE: I think refcounts refer to the host file including metadata, so this is an attempt to include that.
	w.metadataClusters = 1 + w.l1Size + w.l2Blocks + w.refcountBlocks + w.refcountTableClusters
	for {
		before := w.metadataClusters
		w.refcountBlocks = divide(w.totalDataClusters+w.metadataClusters, w.clusterSize/2)
		w.refcountTableClusters = divide(w.refcountBlocks, w.clusterSize/8)
		w.metadataClusters = 1 + w.l1Size + w.l2Blocks + w.refcountBlocks + w.refcountTableClusters
		if w.metadataClusters == before {
			break
		}
	}

	w.l2Offset = w.clusterSize * (1 + w.l1Size + w.refcountTableClusters + w.refcountBlocks)

	err := w.writeHeader()
	if err != nil {
		return err
	}

	err = w.writeRefcountTable()
	if err != nil {
		return err
	}

	err = w.writeRefcountBlocks()
	if err != nil {
		return err
	}

	err = w.writeL1Table()
	if err != nil {
		return err
	}

	err = w.writeL2Tables()
	if err != nil {
		return err
	}

	return nil
}

type Header struct {
	Magic                 uint32 //     [0:3] magic: QCOW magic string ("QFI\xfb")
	Version               uint32 //     [4:7] Version number
	BackingFileOffset     uint64 //    [8:15] Offset into the image file at which the backing file name is stored.
	BackingFileSize       uint32 //   [16:19] Length of the backing file name in bytes.
	ClusterBits           uint32 //   [20:23] Number of bits that are used for addressing an offset whithin a cluster.
	Size                  uint64 //   [24:31] Virtual disk size in bytes
	CryptMethod           uint32 //   [32:35] Crypt method
	L1Size                uint32 //   [36:39] Number of entries in the active L1 table
	L1TableOffset         uint64 //   [40:47] Offset into the image file at which the active L1 table starts
	RefcountTableOffset   uint64 //   [48:55] Offset into the image file at which the refcount table starts
	RefcountTableClusters uint32 //   [56:59] Number of clusters that the refcount table occupies
	NbSnapshots           uint32 //   [60:63] Number of snapshots contained in the image
	SnapshotsOffset       uint64 //   [64:71] Offset into the image file at which the snapshot table starts
	IncompatibleFeatures  uint64 //   [72:79] for version >= 3: Bitmask of incomptible feature
	CompatibleFeatures    uint64 //   [80:87] for version >= 3: Bitmask of compatible feature
	AutoclearFeatures     uint64 //   [88:95] for version >= 3: Bitmask of auto-clear feature
	RefcountOrder         uint32 //   [96:99] for version >= 3: Describes the width of a reference count block entry
	HeaderLength          uint32 // [100:103] for version >= 3: Length of the header structure in bytes
}

func (w *Writer) writeHeader() error {

	hdr := &Header{
		Magic:                 0x514649FB,
		Version:               2,
		ClusterBits:           16, // Number of trailing zeroes on the w.clusterSize in binary (sys.Ctz)
		Size:                  uint64(w.h.Size()),
		L1Size:                uint32(w.l2Blocks), // TODO: should this be divided by 8? (bytes per entry)
		L1TableOffset:         uint64((1 + w.refcountBlocks + w.refcountTableClusters) * w.clusterSize),
		RefcountTableOffset:   uint64(w.clusterSize),
		RefcountTableClusters: uint32(w.refcountTableClusters),
		// SnapshotsOffset:       uint64(w.snapshotsOffset),
	}

	err := binary.Write(w.w, binary.BigEndian, hdr)
	if err != nil {
		return err
	}

	_, err = w.w.Seek(w.clusterSize, io.SeekStart)
	if err != nil {
		return err
	}

	return nil

}

func (w *Writer) writeRefcountTable() error {

	buf := new(bytes.Buffer)
	first := w.clusterSize * (1 + w.refcountTableClusters)
	offset := first
	for block := int64(0); block < w.refcountBlocks; block++ {
		err := binary.Write(buf, binary.BigEndian, uint64(offset))
		if err != nil {
			return err
		}
		offset += w.clusterSize
	}

	_, err := io.Copy(w.w, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return err
	}

	_, err = w.w.Seek(first, io.SeekStart)
	if err != nil {
		return err
	}

	return nil

}

func (w *Writer) writeRefcountBlocks() error {

	buf := new(bytes.Buffer)

	// NOTE: we also calculate cluster offsets here to avoid checking for holes twice
	clusterOffset := w.l2Offset + w.l2Blocks*w.clusterSize
	w.clusterOffsets = make([]int64, w.totalDataClusters)
	w.clusterInUse = make([]bool, w.totalDataClusters)

	for cluster := int64(0); cluster < w.metadataClusters; cluster++ {
		refs := uint16(1)
		err := binary.Write(buf, binary.BigEndian, refs)
		if err != nil {
			return err
		}
	}

	for cluster := int64(0); cluster < w.totalDataClusters; cluster++ {
		w.clusterOffsets[cluster] = clusterOffset
		refs := uint16(0)
		if !w.h.RegionIsHole(cluster*w.clusterSize, w.clusterSize) {
			refs = 1
			w.clusterInUse[cluster] = true
			clusterOffset += w.clusterSize
		}
		err := binary.Write(buf, binary.BigEndian, refs)
		if err != nil {
			return err
		}
	}

	_, err := io.Copy(w.w, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return err
	}

	_, err = w.w.Seek((1+w.refcountBlocks+w.refcountTableClusters)*w.clusterSize, io.SeekStart)
	if err != nil {
		return err
	}

	return nil

}

func (w *Writer) writeL1Table() error {

	l2Capacity := w.clusterSize * (w.clusterSize / 8)

	buf := new(bytes.Buffer)

	var l2 int64
	var capacity int64
	var required = w.h.Size()

	for capacity < required {
		l2Offset := w.l2Offset + w.clusterSize*l2

		if w.h.RegionIsHole(l2*w.clusterSize, w.clusterSize) {
			err := binary.Write(buf, binary.BigEndian, uint64(0))
			if err != nil {
				return err
			}
		} else {
			err := binary.Write(buf, binary.BigEndian, uint64(l2Offset)|(1<<63)) // OFLAG_COPIED
			if err != nil {
				return err
			}
		}
		l2++
		capacity += l2Capacity
	}

	_, err := io.Copy(w.w, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return err
	}

	_, err = w.w.Seek(w.l2Offset, io.SeekStart)
	if err != nil {
		return err
	}

	return nil

}

func (w *Writer) writeL2Tables() error {

	buf := new(bytes.Buffer)
	for cluster := int64(0); cluster < w.totalDataClusters; cluster++ {
		offset := uint64(0)
		if w.clusterInUse[cluster] {
			offset = uint64(w.clusterOffsets[cluster]) | (1 << 63) // OFLAG_COPIED
		}
		err := binary.Write(buf, binary.BigEndian, offset)
		if err != nil {
			return err
		}
	}

	_, err := io.Copy(w.w, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return err
	}

	_, err = w.w.Seek(w.clusterOffsets[0], io.SeekStart)
	if err != nil {
		return err
	}

	return nil

}

func (w *Writer) Close() error {
	return nil
}

func (w *Writer) Write(p []byte) (int, error) {

	k, err := w.w.Write(p)
	w.cursor += int64(k)
	return k, err

	// TODO: check not writing into a forbidden cluster
}

func (w *Writer) Seek(offset int64, whence int) (int64, error) {
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

	cluster := abs / w.clusterSize
	delta := abs % w.clusterSize
	x := w.clusterOffsets[cluster] + delta
	_, err := w.w.Seek(x, io.SeekStart)
	w.cursor = abs
	if err != nil {
		return 0, err
	}
	return abs, nil
}
