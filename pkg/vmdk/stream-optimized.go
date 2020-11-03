package vmdk

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/vorteil/vorteil/pkg/vio"
)

// Sizer is an interface that shouldn't exist in a vacuum, but does because our
// other image formats follow a similar patten and need more information. A
// Sizer should return the true and final RAW size of the image and be callable
// before the first byte of data is written to the Writer. Note that our
// vimg.Builder implements this interface and is the intended argument in most
// cases.
type Sizer interface {
	Size() int64
}

// StreamOptimizedWriter implements io.Closer, io.Writer, and io.Seeker
// interfaces. Creating a stream-optimized VMDK image is as simple as getting
// one of these writers and copying a raw image into it.
type StreamOptimizedWriter struct {
	w io.WriteSeeker
	h Sizer

	hdr         *Header
	grainBuffer *bytes.Buffer
	space       int64
	cursor      int64

	streamTable        []uint32
	streamDirectory    []uint32
	streamCurrentTable int64
	totalDataSectors   int64
	totalDataGrains    int64
	totalTables        int64
	totalGDSectors     int64
	totalGTSectors     int64
	grainNo            int64
	grainCounter       int64
}

func streamDescriptor(name string, totalDataGrains int64) string {

	template := `# Disk DescriptorFile
version=1
CID=%s
parentCID=ffffffff
createType="streamOptimized"

# Extent description
RW %d SPARSE "%s.vmdk"

# The Disk Data Base
#DDB

ddb.virtualHWVersion = "8"
ddb.adapterType = "ide"
`

	uid := generateDiskUID()
	description := fmt.Sprintf(template, uid, totalDataGrains*SectorsPerGrain, name)
	return description
}

func (w *StreamOptimizedWriter) writeStreamHeader() error {
	hdr := new(Header)
	hdr.MagicNumber = Magic
	hdr.Version = 3
	hdr.Flags = 0x30001
	hdr.GrainSize = SectorsPerGrain
	hdr.DescriptorOffset = 1
	hdr.DescriptorSize = 20
	hdr.NumGTEsPerGT = TableMaxRows
	hdr.RGDOffset = 0
	hdr.SingleEndLineChar = '\n'
	hdr.NonEndLineChar = ' '
	hdr.DoubleEndLineChar1 = '\r'
	hdr.DoubleEndLineChar2 = '\n'
	hdr.CompressAlgorithm = 1

	w.totalTables = (w.totalDataGrains + TableMaxRows - 1) / TableMaxRows

	w.totalGDSectors = (w.totalTables*4 + SectorSize - 1) / SectorSize
	w.totalGTSectors = w.totalTables * TableSectors

	// GDOffset comes after the redundant grain directory
	// and its grain tables.
	hdr.GDOffset = 0xFFFFFFFFFFFFFFFF

	// Overhead is measured in grains, not sectors.
	// Includes everything before the start of the disk contents.
	hdr.OverHead = 128

	hdr.Capacity = uint64(w.totalDataSectors)

	w.hdr = hdr

	err := binary.Write(w.w, binary.LittleEndian, hdr)
	if err != nil {
		return err
	}

	return nil
}

func (w *StreamOptimizedWriter) init() error {

	var err error
	w.streamTable = make([]uint32, 512)
	w.totalDataSectors = (w.h.Size() + SectorSize - 1) / SectorSize
	w.totalDataGrains = (w.totalDataSectors + SectorsPerGrain - 1) / SectorsPerGrain

	// write header
	err = w.writeStreamHeader()
	if err != nil {
		return err
	}

	// write descriptor
	name := "disk"
	description := streamDescriptor(name, w.totalDataGrains)
	_, err = io.Copy(w.w, strings.NewReader(description))
	if err != nil {
		return err
	}

	_, err = w.w.Seek(GrainSize, io.SeekStart)
	if err != nil {
		return err
	}

	w.grainBuffer = bytes.NewBuffer(make([]byte, GrainSize, GrainSize))
	w.grainBuffer.Reset()
	w.space = GrainSize
	w.grainNo = 0

	return nil
}

func compress(grain []byte) ([]byte, error) {

	buf := new(bytes.Buffer)

	// NOTE: contrary to the spec I've seen, RFC 1950 seems to be the correct
	// compression algorithm.

	// RFC 1950
	w, _ := zlib.NewWriterLevel(buf, zlib.NoCompression)

	_, err := w.Write(grain)
	if err != nil {
		return nil, err
	}

	err = w.Close()
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil

}

type grainMarker struct {
	LBA  uint64
	Size uint32
}

func (w *StreamOptimizedWriter) flushGrain() error {
	var err error
	defer func() {
		w.grainNo++
		w.cursor = w.grainNo * GrainSize
		w.space = GrainSize
		w.grainBuffer.Reset()
	}()

	// flush table if necessary
	if w.grainNo/TableMaxRows != w.streamCurrentTable {
		var writeTable bool
		for _, x := range w.streamTable {
			if x != 0 {
				writeTable = true
				break
			}
		}

		if !writeTable {
			w.streamDirectory = append(w.streamDirectory, 0)
			w.streamCurrentTable++
		} else {
			// write table marker to disk
			marker := make([]uint32, 128)
			marker[0] = 0x04
			marker[3] = 0x01

			pos, err := w.w.Seek(0, io.SeekCurrent)
			if err != nil {
				return err
			}

			// write table to disk
			err = binary.Write(w.w, binary.LittleEndian, append(marker, w.streamTable...))
			if err != nil {
				return err
			}

			// add table location to directory
			offset := uint32(pos / SectorSize)
			w.streamDirectory = append(w.streamDirectory, offset+1)
			_, err = w.w.Seek(pos+512+2048, io.SeekStart)
			if err != nil {
				return err
			}

			// reset table buffer
			w.streamTable = make([]uint32, 512)
			w.streamCurrentTable++
		}

	}

	// skip if grain is empty
	grain := w.grainBuffer.Bytes()
	empty := true
	if len(grain) != 0 {
		for _, x := range grain {
			if x != 0 {
				empty = false
				break
			}
		}
	}

	if empty {
		return nil
	}

	if len(grain) < GrainSize {
		_, err = io.CopyN(w.grainBuffer, vio.Zeroes, GrainSize-int64(len(grain)))
		if empty {
			return nil
		}
		grain = w.grainBuffer.Bytes()
	}

	w.grainCounter++

	// compress grain
	compressed, err := compress(grain)
	if err != nil {
		return err
	}

	// write grain marker
	pos, err := w.w.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	offset := pos / SectorSize
	lba := int64(SectorsPerGrain * w.grainNo)

	marker := new(grainMarker)
	marker.LBA = uint64(lba)
	marker.Size = uint32(len(compressed))

	err = binary.Write(w.w, binary.LittleEndian, marker)
	if err != nil {
		return err
	}

	// write grain
	_, err = w.w.Write(compressed)
	if err != nil {
		return err
	}

	// pad to sector
	pad := SectorSize - (12+int64(len(compressed)))%SectorSize
	_, err = w.w.Seek(pad, io.SeekCurrent)
	if err != nil {
		return err
	}

	w.streamTable[w.grainNo%512] = uint32(offset)

	return nil
}

// Write implements io.Writer.
func (w *StreamOptimizedWriter) Write(p []byte) (int, error) {

	if w.cursor >= w.h.Size() {
		return 0, io.EOF
	}

	if int64(len(p)) < w.space {
		k, err := w.grainBuffer.Write(p)
		w.cursor += int64(k)
		w.space -= int64(k)
		return k, err
	}

	delta := w.space
	k, err := w.grainBuffer.Write(p[:delta])
	w.cursor += int64(k)
	w.space -= int64(k)

	err = w.flushGrain()
	if err != nil {
		return k, err
	}

	x := k
	p = p[delta:]

	if len(p) == 0 {
		return x, nil
	}

	k, err = w.Write(p)
	k += x
	return k, err
}

// Seek implements io.Seeker.
func (w *StreamOptimizedWriter) Seek(offset int64, whence int) (int64, error) {
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

	if abs < w.cursor {
		return w.cursor, errors.New("stream optimized vmdk writer cannot seek backwards")
	}

	for {
		nextGrainStart := (w.grainNo + 1) * GrainSize
		if abs < nextGrainStart {
			_, err := io.CopyN(w, vio.Zeroes, abs-w.cursor)
			return w.cursor, err
		}

		err := w.flushGrain()
		if err != nil {
			return w.cursor, err
		}

		if abs == w.cursor {
			return w.cursor, nil
		}
	}

}

func (w *StreamOptimizedWriter) writeFooter() error {

	// flush last table
	// write table marker to disk
	marker := make([]uint32, 128)
	marker[0] = 0x04
	marker[3] = 0x01

	// add table location to directory
	pos, err := w.w.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	offset := pos / SectorSize
	w.streamDirectory = append(w.streamDirectory, uint32(offset+1))

	// write table to disk
	err = binary.Write(w.w, binary.LittleEndian, append(marker, w.streamTable...))
	if err != nil {
		return err
	}

	_, err = w.w.Seek(pos+512+2048, io.SeekStart)
	if err != nil {
		return err
	}

	// reset table buffer
	w.streamTable = make([]uint32, 512)
	w.streamCurrentTable++

	// write grain directory
	// write directory marker to disk
	marker[0] = 0x01
	marker[3] = 0x02

	// pad directory to sector alignment
	if len(w.streamDirectory)%128 != 0 {
		w.streamDirectory = append(w.streamDirectory, make([]uint32, 128-len(w.streamDirectory)%128)...)
	}

	// write directory to disk
	pos, err = w.w.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	err = binary.Write(w.w, binary.LittleEndian, append(marker, w.streamDirectory...))
	if err != nil {
		return err
	}

	// add table location to directory
	gdOffset := (pos + 512) / 512
	offset = pos + 512 + 4*int64(len(w.streamDirectory))

	_, err = w.w.Seek(offset, io.SeekStart)
	if err != nil {
		return err
	}

	// write footer marker
	marker[0] = 1
	marker[3] = 3

	err = binary.Write(w.w, binary.LittleEndian, marker)
	if err != nil {
		return err
	}

	// write footer
	w.hdr.GDOffset = uint64(gdOffset)
	err = binary.Write(w.w, binary.LittleEndian, w.hdr)
	if err != nil {
		return err
	}

	return nil

}

// EOSMarker marks the end of the stream-optimized VMDK.
type EOSMarker struct {
	Val  uint64
	Size uint32
	Type uint32
	Pad  [496]byte
}

func (w *StreamOptimizedWriter) writeEOS() error {

	marker := new(EOSMarker)
	err := binary.Write(w.w, binary.LittleEndian, marker)
	if err != nil {
		return err
	}

	return nil

}

// Close implements io.Closer.
func (w *StreamOptimizedWriter) Close() error {

	_, err := w.Seek(w.h.Size(), io.SeekStart)
	if err != nil {
		return err
	}

	err = w.writeFooter()
	if err != nil {
		return err
	}

	err = w.writeEOS()
	if err != nil {
		return err
	}

	return nil

}

// NewStreamOptimizedWriter returns a StreamOptimizedWriter to which a RAW image
// can be copied in order to create an XVA format disk image. The Sizer 'h' must
// accurately return the true and final RAW size of the image.
func NewStreamOptimizedWriter(w io.WriteSeeker, h Sizer) (*StreamOptimizedWriter, error) {

	x := &StreamOptimizedWriter{
		w: w,
		h: h,
	}

	err := x.init()
	if err != nil {
		return nil, err
	}

	return x, nil

}
