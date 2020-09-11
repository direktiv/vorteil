package vdisk

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/vorteil/vorteil/pkg/vimg"
)

type Format string

const (
	RAWFormat                 Format = "raw"
	VMDKFormat                Format = "vmdk"
	VHDFormat                 Format = "vhd"
	VMDKSparseFormat          Format = "vmdk-sparse"
	VMDKStreamOptimizedFormat Format = "vmdk-stream-optimized"
	GCPFArchiveFormat         Format = "gcp"

	// case VHDFormat:
	// case VHDFormat2:
	// case XVAFormat:
)

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
		VHDFormat:                 ".vhd",
		VMDKSparseFormat:          ".vmdk",
		VMDKStreamOptimizedFormat: ".vmdk",
		GCPFArchiveFormat:         ".tar.gz",
	}

	alignments = map[Format]int64{
		RAWFormat:                 0x200000,
		VMDKFormat:                0x200000,
		VHDFormat:                 0x200000,
		VMDKSparseFormat:          0x200000,
		VMDKStreamOptimizedFormat: 0x200000,
		GCPFArchiveFormat:         0x40000000,
	}

	buildFuncs = map[Format]func(context.Context, io.WriteSeeker, *vimg.Builder) error{
		RAWFormat:                 buildRAW,
		VMDKFormat:                buildSparseVMDK,
		VMDKSparseFormat:          buildSparseVMDK,
		VMDKStreamOptimizedFormat: buildStreamOptimizedVMDK,
		GCPFArchiveFormat:         buildGCPArchive,
		// TODO: other formats
	}
)

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

func (x *Format) Suffix() string {
	return formats[*x]
}

func (x *Format) Alignment() int64 {
	return alignments[*x]
}

func (x *Format) build(ctx context.Context, w io.WriteSeeker, b *vimg.Builder) error {
	return buildFuncs[*x](ctx, w, b)
}
