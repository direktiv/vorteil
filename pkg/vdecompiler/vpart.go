package vdecompiler

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"archive/tar"
	"fmt"
	"io"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vmdk"
)

type vpartInfo struct {
	files []*KernelFile
	vcfg  *vcfg.VCFG
}

// KernelFile contains information about a kernel bundle file and its location
// on the image.
type KernelFile struct {
	Name        string
	Size        int
	ImageOffset int
}

func (iio *IO) kernelTAROffset() (int64, error) {

	partitions, err := iio.GPTEntries()
	if err != nil {
		return 0, err
	}

	return int64((partitions[0].FirstLBA + vimg.KernelConfigSpaceSectors) * vmdk.SectorSize), nil

}

func (iio *IO) seekToKernelTAR() (int64, error) {

	offset, err := iio.kernelTAROffset()
	if err != nil {
		return 0, err
	}

	_, err = iio.img.Seek(offset, io.SeekStart)
	if err != nil {
		return 0, err
	}

	return offset, nil

}

// KernelFiles returns a list of every kernel bundle file on the image.
func (iio *IO) KernelFiles() ([]*KernelFile, error) {

	if iio.vpart.files != nil {
		return iio.vpart.files, nil
	}

	offset, err := iio.seekToKernelTAR()
	if err != nil {
		return nil, err
	}

	r := iio.img
	var kfiles = make([]*KernelFile, 0)
	tr := tar.NewReader(r)

	for {

		hdr, err := tr.Next()
		if err == io.EOF {
			iio.vpart.files = kfiles
			return iio.vpart.files, nil
		}
		if err != nil {
			return nil, err
		}

		kf := new(KernelFile)
		kf.Name = hdr.Name
		kf.Size = int(hdr.Size)

		offset += vmdk.SectorSize
		kf.ImageOffset = int(offset)
		offset += ((int64(hdr.Size) + vmdk.SectorSize - 1) / vmdk.SectorSize) * vmdk.SectorSize

		kfiles = append(kfiles, kf)

	}

}

// KernelFile returns the a reader for a kernel bundle file on the image.
func (iio *IO) KernelFile(name string) (io.Reader, error) {

	kfiles, err := iio.KernelFiles()
	if err != nil {
		return nil, err
	}

	idx := -1

	for i, kf := range kfiles {
		if kf.Name == name {
			idx = i
			break
		}
	}

	if idx < 0 {
		return nil, fmt.Errorf("kernel file not found: %s", name)
	}

	_, err = iio.img.Seek(int64(kfiles[idx].ImageOffset), io.SeekStart)
	if err != nil {
		return nil, err
	}

	r := io.LimitReader(iio.img, int64(kfiles[idx].Size))
	return r, nil

}
