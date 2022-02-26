package ova

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"archive/tar"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"

	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vmdk"

	"github.com/vorteil/vorteil/pkg/vcfg"
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

// ImageFormatOVA ...
const ImageFormatOVA = vdisk.Format("ova")

// Writer implements io.Closer, io.Writer, and io.Seeker interfaces. Creating an
// OVA image is as simple as getting one of these writers and copying a raw
// image into it.
type Writer struct {
	tw *tar.Writer

	vmdkWriter  *vmdk.StreamOptimizedWriter
	vmdkTmpFile *os.File

	ovaFileName string

	h   Sizer
	cfg *vcfg.VCFG
}

// NewWriter Creates a tar writer and a VMDK Stream Optimized Writer.
// The OVA writer wraps the VMDK writer and will write the raw image
// directly to it. The OVA will only be constructed on the .Close()
// function, and will do so by adding the built vmdk and a generated ovf
// to a tar archive. The vmdk writer writes to a temp file.
func NewWriter(w io.Writer, h Sizer, cfg *vcfg.VCFG) (*Writer, error) {
	var err error
	xw := new(Writer)
	xw.h = h
	xw.cfg = cfg
	xw.tw = tar.NewWriter(w)
	xw.ovaFileName = "vorteil"

	reg, err := regexp.Compile("[^a-zA-Z0-9!-).]+")
	if err != nil {
		panic(err) // This should never happen
	}

	if vcfgInfoName := reg.ReplaceAllString(cfg.Info.Name, ""); vcfgInfoName != "" {
		// VCFG Info Name is set and valid
		xw.ovaFileName = vcfgInfoName
	}

	xw.vmdkTmpFile, err = ioutil.TempFile(os.TempDir(), "vorteil-*.vmdk")
	if err != nil {
		return nil, err
	}

	xw.vmdkWriter, err = vmdk.NewStreamOptimizedWriter(xw.vmdkTmpFile, h)
	return xw, err
}

// Write implements io.Writer. (Wraps Stream Optimised VMDK Writer)
func (w *Writer) Write(p []byte) (int, error) {
	return w.vmdkWriter.Write(p)
}

// Seek implements io.Seeker. (Wraps Stream Optimised VMDK Writer)
func (w *Writer) Seek(offset int64, whence int) (int64, error) {
	return w.vmdkWriter.Seek(offset, whence)
}

// Close does the following steps:
// 1) Close VMDK writer
// 2) Generate OVF
// 3) Tar OVF and VMDK, and write that to the OVA
// 4) Clean up VMDK in temp directory.
func (w *Writer) Close() error {
	defer w.tw.Close()
	defer os.Remove(w.vmdkTmpFile.Name())
	err := w.vmdkWriter.Close()
	if err != nil {
		return fmt.Errorf("failed to close ova - vmdk writer, %v", err)
	}
	vmdkName := w.vmdkTmpFile.Name()
	w.vmdkTmpFile.Close()

	// TAR - OVF
	ovf := GenerateOVF(w.ovaFileName+".vmdk", w.cfg, int(w.h.Size()))
	hdr := &tar.Header{
		Name: w.ovaFileName + ".ovf",
		Mode: 0600,
		Size: int64(ovf.Size()),
	}

	if err := w.tw.WriteHeader(hdr); err != nil {
		return err
	}

	_, err = io.Copy(w.tw, ovf)
	if err != nil {
		return err
	}

	// TAR - VMDK
	srcFile, err := os.Open(vmdkName)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	fi, err := srcFile.Stat()
	if err != nil {
		return err
	}

	hdr = &tar.Header{
		Name: w.ovaFileName + ".vmdk",
		Mode: 0600,
		Size: fi.Size(),
	}

	if err := w.tw.WriteHeader(hdr); err != nil {
		return err
	}

	if _, err = io.Copy(w.tw, srcFile); err != nil {
		return err
	}

	return w.tw.Close()
}
