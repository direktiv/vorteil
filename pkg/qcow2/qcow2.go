package qcow2

import (
	"bytes"
	"encoding/binary"
	"io"

	"github.com/zchee/go-qcow2"
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

	w.refcountBlocks = divide(w.totalDataClusters, w.clusterSize/2)
	w.refcountTableClusters = divide(w.refcountBlocks, w.clusterSize/8)

	w.l2Size = w.clusterSize
	w.l2Blocks = divide(w.totalDataClusters, w.clusterSize/8)
	w.l1Size = divide(w.l2Blocks, w.clusterSize/8)

	w.l2Offset = w.clusterSize * (1 + w.l1Size + w.refcountTableClusters + w.refcountBlocks)

	err := w.writeHeader()
	if err != nil {
		return err
	}

	err = w.writeL1Table()
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

	err = w.writeL2Tables()
	if err != nil {
		return err
	}

	return nil
}

func (w *Writer) writeHeader() error {

	hdr := &qcow2.Header{
		Magic:                 qcow2.BEUint32(qcow2.MAGIC),
		Version:               qcow2.Version2,
		ClusterBits:           16, // Number of trailing zeroes on the w.clusterSize in binary (sys.Ctz)
		Size:                  uint64(w.h.Size()),
		L1Size:                uint32(w.l1Size), // TODO: should this be divided by 8? (bytes per entry)
		L1TableOffset:         uint64(w.clusterSize),
		RefcountTableOffset:   uint64(w.clusterSize * (1 + w.l1Size)),
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

func (w *Writer) writeL1Table() error {

	l2Capacity := w.clusterSize * (w.clusterSize / 8)

	buf := new(bytes.Buffer)

	var l2 int64
	var capacity int64
	var required = w.h.Size()

	for capacity < required {
		l2Offset := w.l2Offset + w.clusterSize*l2
		l2++
		err := binary.Write(buf, binary.BigEndian, uint64(l2Offset)|qcow2.OFLAG_COPIED)
		if err != nil {
			return err
		}
		capacity += l2Capacity
	}

	_, err := io.Copy(w.w, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return err
	}

	_, err = w.w.Seek((w.l1Size+1)*w.clusterSize, io.SeekStart)
	if err != nil {
		return err
	}

	return nil

}

func (w *Writer) writeRefcountTable() error {

	buf := new(bytes.Buffer)
	first := w.clusterSize * (1 + w.l1Size + w.refcountTableClusters)
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
			offset = uint64(w.clusterOffsets[cluster]) | qcow2.OFLAG_COPIED
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
