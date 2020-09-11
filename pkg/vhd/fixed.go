package vhd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"time"

	"github.com/vorteil/vorteil/pkg/vio"
)

type HolePredictor interface {
	Size() int64
	RegionIsHole(begin, size int64) bool
}

type FixedWriter struct {
	w      io.WriteSeeker
	cursor int64
	length int64
}

func NewFixedWriter(w io.WriteSeeker, h HolePredictor) (*FixedWriter, error) {
	return &FixedWriter{
		w:      w,
		length: h.Size(),
	}, nil
}

func (w *FixedWriter) Write(p []byte) (n int, err error) {
	n, err = w.Write(p)
	w.cursor += int64(n)
	return
}

func (w *FixedWriter) Seek(offset int64, whence int) (int64, error) {
	k, err := w.w.Seek(offset, whence)
	w.cursor = k
	return k, err
}

func (w *FixedWriter) writeFooter() error {

	var err error
	_, err = w.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}

	if w.cursor < w.length {
		return errors.New("vhd fixed image writer expected more raw image data than was received")
	}

	conectix := uint64(0x636F6E6563746978)
	timestamp := time.Now().Unix() - 946684800 // 2000 offset

	// CHS crap
	var cylinders, heads, sectorsPerTrack int64
	var cylinderTimesHeads int64

	totalSectors := w.length / 512
	if totalSectors > 65535*16*255 {
		totalSectors = 65535 * 16 * 255
	}

	if totalSectors >= 65525*16*63 {
		sectorsPerTrack = 255
		heads = 16
		cylinderTimesHeads = totalSectors / sectorsPerTrack
	} else {
		sectorsPerTrack = 17
		cylinderTimesHeads = totalSectors / sectorsPerTrack
		heads = (cylinderTimesHeads + 1023) / 1024
		if heads < 4 {
			heads = 4
		}
		if cylinderTimesHeads >= (heads*1024) || heads > 16 {
			sectorsPerTrack = 31
			heads = 16
			cylinderTimesHeads = totalSectors / sectorsPerTrack
		}
		if cylinderTimesHeads >= heads*1024 {
			sectorsPerTrack = 63
			heads = 16
			cylinderTimesHeads = totalSectors / sectorsPerTrack
		}
	}
	cylinders = cylinderTimesHeads / heads

	// copy of hard disk footer
	footer := &footer{
		Cookie:             conectix,
		Features:           0x00000002,
		FileFormatVersion:  0x00010000,
		DataOffset:         0xFFFFFFFFFFFFFFFF,
		TimeStamp:          uint32(timestamp),
		CreatorApplication: 0x76636C69,
		CreatorVersion:     0x00010000, // TODO: does this matter?
		CreatorHostOS:      0x5769326B, // TODO: does this matter?
		OriginalSize:       uint64(w.length),
		CurrentSize:        uint64(w.length),
		DiskGeometry:       uint32(cylinders<<16 | heads<<8 | sectorsPerTrack),
		DiskType:           2, // fixed vhd
		// TODO: UniqueID
	}

	buf := new(bytes.Buffer)
	err = binary.Write(buf, binary.BigEndian, footer)
	if err != nil {
		return err
	}

	var checksum uint32

	for i := 0; i < buf.Len(); i++ {
		var x byte
		x, err = buf.ReadByte()
		if err != nil {
			return err
		}
		checksum += uint32(x) // TODO: does this achieve one's complement?
	}

	footer.Checksum = ^checksum

	fbuf := new(bytes.Buffer)
	err = binary.Write(fbuf, binary.BigEndian, footer)
	if err != nil {
		return err
	}

	_, err = io.Copy(w.w, bytes.NewReader(fbuf.Bytes()))
	if err != nil {
		return err
	}

	return nil
}

func (w *FixedWriter) Close() error {

	err := w.writeFooter()
	if err != nil {
		return err
	}

	return nil

}

type fixedStreamWrapper struct {
	io.Writer
	raw vio.File
}

func (w *fixedStreamWrapper) wrap() error {
	_, err := io.Copy(w, w.raw)
	if err != nil {
		return err
	}

	conectix := uint64(0x636F6E6563746978)
	// cxsparse := uint64(0x6378737061727365)
	timestamp := time.Now().Unix() - 946684800 // 2000 offset

	// CHS crap
	var cylinders, heads, sectorsPerTrack int
	var cylinderTimesHeads int

	totalSectors := w.raw.Size() / 512
	if totalSectors > 65535*16*255 {
		totalSectors = 65535 * 16 * 255
	}

	if totalSectors >= 65525*16*63 {
		sectorsPerTrack = 255
		heads = 16
		cylinderTimesHeads = totalSectors / sectorsPerTrack
	} else {
		sectorsPerTrack = 17
		cylinderTimesHeads = totalSectors / sectorsPerTrack
		heads = (cylinderTimesHeads + 1023) / 1024
		if heads < 4 {
			heads = 4
		}
		if cylinderTimesHeads >= (heads*1024) || heads > 16 {
			sectorsPerTrack = 31
			heads = 16
			cylinderTimesHeads = totalSectors / sectorsPerTrack
		}
		if cylinderTimesHeads >= heads*1024 {
			sectorsPerTrack = 63
			heads = 16
			cylinderTimesHeads = totalSectors / sectorsPerTrack
		}
	}
	cylinders = cylinderTimesHeads / heads

	// copy of hard disk footer
	footer := &footer{
		Cookie:             conectix,
		Features:           0x00000002,
		FileFormatVersion:  0x00010000,
		DataOffset:         0xFFFFFFFFFFFFFFFF,
		TimeStamp:          uint32(timestamp),
		CreatorApplication: 0x76636C69,
		CreatorVersion:     0x00010000, // TODO: does this matter?
		CreatorHostOS:      0x5769326B, // TODO: does this matter?
		OriginalSize:       uint64(w.raw.Size()),
		CurrentSize:        uint64(w.raw.Size()),
		DiskGeometry:       uint32(cylinders<<16 | heads<<8 | sectorsPerTrack),
		DiskType:           2, // fixed vhd
		// TODO: UniqueID
	}

	buf := new(bytes.Buffer)
	err = binary.Write(buf, binary.BigEndian, footer)
	if err != nil {
		return err
	}

	var checksum uint32

	for i := 0; i < buf.Len(); i++ {
		var x byte
		x, err = buf.ReadByte()
		if err != nil {
			return err
		}
		checksum += uint32(x) // TODO: does this achieve one's complement?
	}

	footer.Checksum = ^checksum

	fbuf := new(bytes.Buffer)
	err = binary.Write(fbuf, binary.BigEndian, footer)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, bytes.NewReader(fbuf.Bytes()))
	if err != nil {
		return err
	}
	return nil
}

// WrapFixed ..
func WrapFixed(w io.Writer, f vio.File) error {

	var err error

	wrapper := &fixedStreamWrapper{
		Writer: w,
		raw:    f,
	}

	err = wrapper.wrap()
	if err != nil {
		return err
	}

	return nil

}
