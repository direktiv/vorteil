package vhd

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

const chunkSize = 0x200000

type DynamicWriter struct {
	w             io.WriteSeeker
	h             HolePredictor
	header        *header
	footer        *bytes.Buffer
	cursor        int64
	chunkOffsets  []int64
	flushedChunks int64
}

func NewDynamicWriter(w io.WriteSeeker, h HolePredictor) (*DynamicWriter, error) {

	dw := new(DynamicWriter)
	dw.w = w
	dw.h = h
	dw.chunkOffsets = make([]int64, (dw.h.Size()+chunkSize-1)/chunkSize)

	err := dw.writeRedundantFooter()
	if err != nil {
		return nil, err
	}

	err = dw.writeHeader()
	if err != nil {
		return nil, err
	}

	err = dw.writeBAT()
	if err != nil {
		return nil, err
	}

	return dw, nil

}

func (w *DynamicWriter) writeRedundantFooter() error {
	conectix := uint64(0x636F6E6563746978)
	timestamp := time.Now().Unix() - 946684800 // 2000 offset

	// CHS crap
	var cylinders, heads, sectorsPerTrack int64
	var cylinderTimesHeads int64

	totalSectors := w.h.Size() / 512
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
		DataOffset:         512,
		TimeStamp:          uint32(timestamp),
		CreatorApplication: 0x76636C69,
		CreatorVersion:     0x00010000, // TODO: does this matter?
		CreatorHostOS:      0x5769326B, // TODO: does this matter?
		OriginalSize:       uint64(w.h.Size()),
		CurrentSize:        uint64(w.h.Size()),
		DiskGeometry:       uint32(cylinders<<16 | heads<<8 | sectorsPerTrack),
		DiskType:           3,
		// TODO: UniqueID
	}

	buf := new(bytes.Buffer)
	err := binary.Write(buf, binary.BigEndian, footer)
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

	w.footer = fbuf

	_, err = io.Copy(w.w, bytes.NewReader(fbuf.Bytes()))
	if err != nil {
		return err
	}

	return nil
}

func (w *DynamicWriter) writeHeader() error {
	// sparse drive header
	cxsparse := uint64(0x6378737061727365)
	header := &header{
		Cookie:          cxsparse,
		DataOffset:      0xFFFFFFFFFFFFFFFF,
		TableOffset:     1536,
		HeaderVersion:   0x00010000,
		MaxTableEntries: uint32(w.h.Size() / chunkSize),
		BlockSize:       0x200000,
	}

	hbuf := new(bytes.Buffer)
	err := binary.Write(hbuf, binary.BigEndian, header)
	if err != nil {
		return err
	}

	checksum := uint32(0)

	for i := 0; i < hbuf.Len(); i++ {
		var x byte
		x, err = hbuf.ReadByte()
		if err != nil {
			return err
		}
		checksum += uint32(x) // TODO: does this achieve one's complement?
	}

	header.Checksum = ^checksum

	hbuf = new(bytes.Buffer)
	err = binary.Write(hbuf, binary.BigEndian, header)
	if err != nil {
		return err
	}

	_, err = io.Copy(w.w, bytes.NewReader(hbuf.Bytes()))
	if err != nil {
		return err
	}

	w.header = header

	return nil
}

func (w *DynamicWriter) writeBAT() error {

	// block allocation table
	batEntries := w.header.MaxTableEntries
	batSize := ((4*batEntries + 511) / 512) * 512
	dataStart := int(w.header.TableOffset) + int(batSize)
	bat := bytes.Repeat([]byte{255}, int(batSize))
	offset := int64(dataStart)
	for i := 0; i < int(batEntries); i++ {
		w.chunkOffsets[i] = offset
		if w.h.RegionIsHole(int64(i)*chunkSize, chunkSize) {
			binary.BigEndian.PutUint32(bat[4*i:4*(i+1)], uint32(0xFFFFFFFF))
			continue
		}
		binary.BigEndian.PutUint32(bat[4*i:4*(i+1)], uint32(offset/512))
		offset += 512 + chunkSize
	}

	_, err := io.Copy(w.w, bytes.NewReader(bat))
	if err != nil {
		return err
	}

	return nil
}

func (w *DynamicWriter) Write(p []byte) (n int, err error) {

	chunk := w.cursor / chunkSize
	delta := w.cursor % chunkSize

	endCursor := w.cursor + int64(len(p))
	lastChunk := endCursor / chunkSize
	if endCursor%chunkSize == 0 {
		lastChunk--
	}

	for chunk <= lastChunk {

		var k int64

		if delta == 0 {

			// check that offset matched BAT
			k, err = w.w.Seek(0, io.SeekCurrent)
			if err != nil {
				return
			}

			if w.chunkOffsets[chunk] == k {
				// write bitmap
				_, err = io.Copy(w.w, bytes.NewReader(bytes.Repeat([]byte{255}, 512)))
				if err != nil {
					return
				}
			}
		}

		// write data
		k, err = io.CopyN(w.w, bytes.NewReader(p), chunkSize-delta)
		n += int(k)
		w.cursor += int64(k)
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			return
		}

		err = nil
		p = p[k:]
		delta = 0
		chunk++
	}

	return

}

func (w *DynamicWriter) Seek(offset int64, whence int) (int64, error) {

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

	chunk := abs / chunkSize
	delta := abs % chunkSize
	var trueOffset int64
	l := int64(len(w.chunkOffsets))

	if chunk > l || (chunk == l && delta > 0) {
		return l * chunkSize, io.EOF
	} else if chunk == l {
		trueOffset = l * chunkSize
	} else {
		trueOffset = w.chunkOffsets[chunk] + 512 + delta
	}

	currentChunk := w.cursor / chunkSize

	// chunk bitmaps for every chunk we've skipped
	for {
		curr, err := w.w.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, err
		}

		if curr >= trueOffset {
			break
		}

		if currentChunk >= l {
			break
		}

		if curr <= w.chunkOffsets[currentChunk] {
			_, err = w.w.Seek(w.chunkOffsets[currentChunk], io.SeekStart)
			if err != nil {
				return 0, err
			}

			_, err = io.Copy(w.w, bytes.NewReader(bytes.Repeat([]byte{255}, 512)))
			if err != nil {
				return 0, err
			}
		}

		currentChunk++
	}

	_, err := w.w.Seek(trueOffset, io.SeekStart)
	if err != nil {
		return 0, err
	}

	if w.cursor < abs {
		w.cursor = abs
	}

	return abs, nil

}

func (w *DynamicWriter) writeFooter() error {
	_, err := io.Copy(w.w, bytes.NewReader(w.footer.Bytes()))
	if err != nil {
		return err
	}

	return nil
}

func (w *DynamicWriter) Close() error {

	if w.cursor < w.h.Size() {
		return errors.New("xva archive expected more raw image data than was received")
	}

	err := w.writeFooter()
	if err != nil {
		return err
	}

	return nil
}
