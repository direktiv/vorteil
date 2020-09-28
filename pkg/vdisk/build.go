package vdisk

import (
	"context"
	"fmt"
	"io"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/gcparchive"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vhd"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/vmdk"
	"github.com/vorteil/vorteil/pkg/vpkg"
	"github.com/vorteil/vorteil/pkg/xva"
)

// KernelOptions contains all kernel configuration settings.
type KernelOptions struct {
	Shell bool
}

// BuildArgs contains all arguments a caller can use to customize the behaviour
// of the Build function.
type BuildArgs struct {
	PackageReader vpkg.Reader
	Format        Format
	SizeAlign     int64
	KernelOptions KernelOptions
	Logger        elog.View
}

// NegotiateSize prebuilds the minimum amount for a disk.
func NegotiateSize(ctx context.Context, vimgBuilder *vimg.Builder, cfg *vcfg.VCFG, args *BuildArgs) error {
	size := vimgBuilder.MinimumSize()
	if !cfg.VM.DiskSize.IsDelta() {
		if size > int64(cfg.VM.DiskSize.Units(vcfg.Byte)) {
			delta := vcfg.Bytes(size) - cfg.VM.DiskSize
			delta.Align(vcfg.MiB)
			return fmt.Errorf("specified disk size %s insufficient to contain disk contents", delta)
		}
		size = int64(cfg.VM.DiskSize.Units(vcfg.Byte))
	}

	alignment := args.SizeAlign
	if alignment == 0 {
		alignment = 1
	}
	alignment = lcm(args.Format.Alignment(), alignment)
	if size%args.Format.Alignment() != 0 {
		size = (size/alignment + 1) * alignment
	}

	err := vimgBuilder.Prebuild(ctx, size)
	if err != nil {
		return err
	}

	return nil

}

// CreateBuilder creates a vimg.Builder with args provided.
func CreateBuilder(ctx context.Context, args *vimg.BuilderArgs) (*vimg.Builder, error) {
	vimgBuilder, err := vimg.NewBuilder(ctx, args)
	if err != nil {
		return nil, err
	}
	return vimgBuilder, nil
}

func build(ctx context.Context, w io.WriteSeeker, cfg *vcfg.VCFG, args *BuildArgs) error {

	log := args.Logger

	fsCompiler, err := NewFilesystemCompiler(string(cfg.System.Filesystem), log, args.PackageReader.FS(), nil)
	if err != nil {
		return err
	}

	vimgBuilder, err := CreateBuilder(ctx, &vimg.BuilderArgs{
		Kernel: vimg.KernelOptions{
			Shell: args.KernelOptions.Shell,
		},
		FSCompiler: fsCompiler,
		VCFG:       cfg,
		Logger:     log,
	})
	if err != nil {
		return err
	}
	defer vimgBuilder.Close()

	vimgBuilder.SetDefaultMTU(args.Format.DefaultMTU())

	err = NegotiateSize(ctx, vimgBuilder, cfg, args)
	if err != nil {
		return err
	}

	err = args.Format.Build(ctx, log, w, vimgBuilder, cfg)
	if err != nil {
		return err
	}

	return nil

}

// Build writes a virtual disk image to w using the provided args.
func Build(ctx context.Context, w io.WriteSeeker, args *BuildArgs) error {

	vf := args.PackageReader.VCFG()
	defer vf.Close()
	cfg, err := vcfg.LoadFile(vf)
	if err != nil {
		return err
	}
	_ = vf.Close()

	err = build(ctx, w, cfg, args)
	if err != nil {
		return err
	}

	return nil

}

// greatest common divisor (GCD) via Euclidean algorithm
func gcd(a, b int64) int64 {
	for b != 0 {
		t := b
		b = a % b
		a = t
	}
	return a
}

// find Least Common Multiple (LCM) via GCD
func lcm(a, b int64, integers ...int64) int64 {
	result := a * b / gcd(a, b)

	for i := 0; i < len(integers); i++ {
		result = lcm(result, integers[i])
	}

	return result
}

func buildRAW(w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) (io.WriteSeeker, error) {
	return vio.WriteSeeker(w)
}

func buildStreamOptimizedVMDK(w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) (io.WriteSeeker, error) {
	return vmdk.NewStreamOptimizedWriter(w, b)
}

func buildSparseVMDK(w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) (io.WriteSeeker, error) {
	return vmdk.NewSparseWriter(w, b)
}

func buildGCPArchive(w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) (io.WriteSeeker, error) {
	return gcparchive.NewWriter(w, b)
}

func buildXVA(w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) (io.WriteSeeker, error) {
	return xva.NewWriter(w, b, cfg)
}

func buildFixedVHD(w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) (io.WriteSeeker, error) {
	return vhd.NewFixedWriter(w, b)
}

func buildDynamicVHD(w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) (io.WriteSeeker, error) {
	return vhd.NewDynamicWriter(w, b)
}
