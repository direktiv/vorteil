package vdecompiler

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"encoding/binary"
	"errors"
	"io"
	"io/ioutil"

	"github.com/vorteil/vorteil/pkg/vmdk"
)

type vmdkSparseIO struct {
	iio         *IO
	grain       int
	totalGrains int
	grainSize   int
	offset      int
	remainder   int
	gdes        []uint32
	grains      []uint32
	buffer      io.Reader
}

func (sio *vmdkSparseIO) loadGrain(grain int) (io.Reader, error) {
	if grain >= len(sio.grains) {
		panic(errors.New("grain out of bounds"))
	}

	offset := sio.grains[grain]
	_, err := sio.iio.src.Seek(int64(offset)*vmdk.SectorSize, io.SeekStart)
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

func (sio *vmdkSparseIO) readGrainTable(i int) error {

	_, err := sio.iio.src.Seek(int64(sio.gdes[i])*vmdk.SectorSize, io.SeekStart)
	if err != nil {
		return err
	}

	gtes := make([]uint32, 512)
	err = binary.Read(sio.iio.src, binary.LittleEndian, &gtes)
	if err != nil {
		return err
	}

	sio.grains = append(sio.grains, gtes...)

	return nil

}

func (sio *vmdkSparseIO) readGrainTables() error {

	for i := 0; i < len(sio.gdes); i++ {
		err := sio.readGrainTable(i)
		if err != nil {
			return err
		}
	}

	return nil

}

func (sio *vmdkSparseIO) readGrainDirectory() error {

	tables := (sio.totalGrains + 511) / 512
	sio.gdes = make([]uint32, tables)

	_, err := sio.iio.src.Seek(int64(sio.iio.vmdk.GDOffset)*vmdk.SectorSize, io.SeekStart)
	if err != nil {
		return err
	}

	err = binary.Read(sio.iio.src, binary.LittleEndian, &sio.gdes)
	if err != nil {
		return err
	}

	return nil

}

func (iio *IO) vmdkSparseIO() (*partialIO, error) {

	pio := new(partialIO)
	pio.name = iio.src.name
	pio.size = int(iio.vmdk.Capacity) * vmdk.SectorSize
	pio.closer = iio.src.closer

	sio := new(vmdkSparseIO)
	sio.grain = -1
	sio.iio = iio
	sio.grainSize = int(iio.vmdk.GrainSize) * vmdk.SectorSize
	sio.totalGrains = pio.size / sio.grainSize
	pio.reader = sio
	pio.seeker = sio
	pio.writer = sio

	err := sio.readGrainDirectory()
	if err != nil {
		return nil, err
	}

	err = sio.readGrainTables()
	if err != nil {
		return nil, err
	}

	sio.grains = sio.grains[:sio.totalGrains]

	return pio, nil

}
