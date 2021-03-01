package vdisk

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vimg"
)

// Format is a string representing a supported disk image format.
type Format string

// Supported disk image formats.
const (
	// RAWFormat is a disk type that returns "raw"
	RAWFormat Format = "raw"
	// VMDKFormat is a disk type that returns "vmdk"
	VMDKFormat Format = "vmdk"
	// VMDKSparseFormat is a disk type that returns "vmdk-sparse"
	VMDKSparseFormat Format = "vmdk-sparse"
	// VMDKStreamOptimizedFormat is a disk type that returns "vmdk-stream-optimized"
	VMDKStreamOptimizedFormat Format = "vmdk-stream-optimized"
	// GCPFArchiveFormat is a disk type that returns "gcp"
	GCPFArchiveFormat Format = "gcp"
	// XVAFormat is a disk type that returns "xva"
	XVAFormat Format = "xva"
	// VHDFormat is a disk type that returns "vhd"
	VHDFormat Format = "vhd"
	// VHDFixedFormat is a disk type that returns "vhd-fixed"
	VHDFixedFormat Format = "vhd-fixed"
	// VHDDynamicFormat is a disk type that returns "vhd-dynamic"
	VHDDynamicFormat Format = "vhd-dynamic"
	// QCOW2Format is a disk type that returns "qcow2"
	QCOW2Format = "qcow2"
)

// AllFormatStrings returns a list of all supported disk image formats.
func AllFormatStrings() []string {
	strs := make([]string, len(formats))
	i := 0
	for k := range formats {
		strs[i] = k.String()
		i++
	}
	sort.Strings(strs)
	return strs
}

var (
	formats = map[Format]string{
		RAWFormat:                 ".raw",
		VMDKFormat:                ".vmdk",
		VMDKSparseFormat:          ".vmdk",
		VMDKStreamOptimizedFormat: ".vmdk",
		GCPFArchiveFormat:         ".tar.gz",
		XVAFormat:                 ".xva",
		VHDFormat:                 ".vhd",
		VHDFixedFormat:            ".vhd",
		VHDDynamicFormat:          ".vhd",
		QCOW2Format:               ".qcow2",
	}

	alignments = map[Format]int64{
		RAWFormat:                 0x200000,
		VMDKFormat:                0x200000,
		VMDKSparseFormat:          0x200000,
		VMDKStreamOptimizedFormat: 0x200000,
		GCPFArchiveFormat:         0x40000000,
		XVAFormat:                 0x200000,
		VHDFormat:                 0x200000,
		VHDFixedFormat:            0x200000,
		VHDDynamicFormat:          0x200000,
		QCOW2Format:               0x200000,
	}

	defaultMTUs = map[Format]uint{
		RAWFormat:                 1500,
		VMDKFormat:                1500,
		VMDKSparseFormat:          1500,
		VMDKStreamOptimizedFormat: 1500,
		GCPFArchiveFormat:         1460,
		XVAFormat:                 1500,
		VHDFormat:                 1500,
		VHDFixedFormat:            1500,
		VHDDynamicFormat:          1500,
		QCOW2Format:               1500,
	}

	buildFuncs = map[Format]BuildWriterInstantiator{
		RAWFormat:                 buildRAW,
		VMDKFormat:                buildSparseVMDK,
		VMDKSparseFormat:          buildSparseVMDK,
		VMDKStreamOptimizedFormat: buildStreamOptimizedVMDK,
		GCPFArchiveFormat:         buildGCPArchive,
		XVAFormat:                 buildXVA,
		VHDFormat:                 buildFixedVHD,
		VHDFixedFormat:            buildFixedVHD,
		VHDDynamicFormat:          buildDynamicVHD,
		QCOW2Format:               buildQCOW2,
	}
)

// BuildWriterInstantiator is a function that returns a new io.WriteSeeker that
// can be used to handle the writing of a raw image.
type BuildWriterInstantiator func(io.WriteSeeker, *vimg.Builder, *vcfg.VCFG) (io.WriteSeeker, error)

// RegisterNewDiskFormat registers a new disk format that can be used with the vdisk package.
// Example: RegisterNewDiskFormat(Format("vmdk-custom"), ".vmdk", 0x200000, 1500, customVMDKBuilder)
func RegisterNewDiskFormat(format Format, extention string, alignment int64, mtu uint, builderFunc BuildWriterInstantiator) error {
	if _, exists := formats[format]; exists {
		return fmt.Errorf("refusing to register disk format '%s': already registered", format)
	}

	formats[format] = extention
	alignments[format] = alignment
	defaultMTUs[format] = mtu
	buildFuncs[format] = builderFunc
	return nil
}

// String returns a string representation of the Format.
func (x Format) String() string {
	return string(x)
}

// MarshalText implements encoding.TextMarshaler.
func (x Format) MarshalText() (text []byte, err error) {
	return []byte(x.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (x *Format) UnmarshalText(text []byte) error {
	var err error
	*x, err = ParseFormat(string(text))
	if err != nil {
		return err
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (x Format) MarshalJSON() ([]byte, error) {
	return json.Marshal(x.String())
}

// UnmarshalJSON implements json.Unmarshaler.
func (x *Format) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = strings.Trim(s, "\"")
	var err error
	*x, err = ParseFormat(s)
	if err != nil {
		return err
	}
	return nil
}

// ParseFormat resolves a string into a Format.
func ParseFormat(s string) (Format, error) {

	if s == "" {
		return RAWFormat, nil
	}

	original := s

	s = strings.TrimSpace(s)
	s = strings.ToLower(s)

	f := Format(s)
	if _, ok := formats[f]; !ok {
		return RAWFormat, fmt.Errorf("unrecognized virtual disk format '%s'", original)
	}

	return f, nil
}

// Suffix returns an appropriate file extension for files containing the format.
func (x *Format) Suffix() string {
	return formats[*x]
}

// Alignment returns a size in bytes that a RAW image must be aligned to (an
// integer multiple of) to be compatible with the virtual disk image format.
func (x *Format) Alignment() int64 {
	return alignments[*x]
}

// DefaultMTU returns the default MTU setting for the image format.
func (x *Format) DefaultMTU() uint {
	return defaultMTUs[*x]
}

// Build creates the disk for the correct format ...
func (x *Format) Build(ctx context.Context, log elog.View, w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) error {

	p := log.NewProgress(fmt.Sprintf("Initializing %s image file", x), "", 0)
	defer p.Finish(false)

	w, err := buildFuncs[*x](w, b, cfg)
	if err != nil {
		return err
	}
	closer, ok := w.(io.Closer)
	if ok {
		defer closer.Close()
	}

	p.Finish(true)

	err = b.Build(ctx, w)
	if err != nil {
		return err
	}

	return nil

}
