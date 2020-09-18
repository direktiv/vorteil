package vdisk

import (
	"context"
	"fmt"
	"io"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/ext"
	"github.com/vorteil/vorteil/pkg/gcparchive"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vhd"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vmdk"
	"github.com/vorteil/vorteil/pkg/vpkg"
	"github.com/vorteil/vorteil/pkg/xva"
)

type KernelOptions struct {
	Shell bool
}

type BuildArgs struct {
	PackageReader vpkg.Reader
	Format        Format
	SizeAlign     int64
	KernelOptions KernelOptions
	Logger        elog.View
}

func Build(ctx context.Context, w io.WriteSeeker, args *BuildArgs) error {

	defaultMTU := uint(1500)

	switch args.Format {
	case RAWFormat:
	case VMDKFormat:
	case VMDKSparseFormat:
	case VMDKStreamOptimizedFormat:
	case GCPFArchiveFormat:
		defaultMTU = 1460
	case XVAFormat:
	case VHDFormat:
	case VHDFixedFormat:
	case VHDDynamicFormat:
	default:
		return fmt.Errorf("build function does not support this disk format: '%s'", args.Format)
	}

	log := args.Logger

	vf := args.PackageReader.VCFG()
	defer vf.Close()
	cfg, err := vcfg.LoadFile(vf)
	if err != nil {
		return err
	}
	vf.Close()

	vimgBuilder, err := vimg.NewBuilder(ctx, &vimg.BuilderArgs{
		Kernel: vimg.KernelOptions{
			Shell: args.KernelOptions.Shell,
		},
		FSCompiler: ext.NewCompiler(&ext.CompilerArgs{
			FileTree: args.PackageReader.FS(),
		}),
		VCFG:   cfg,
		Logger: log,
	})
	if err != nil {
		return err
	}
	defer vimgBuilder.Close()

	vimgBuilder.SetDefaultMTU(defaultMTU)

	size := vimgBuilder.MinimumSize()
	if !cfg.VM.DiskSize.IsDelta() {
		if size > int64(cfg.VM.DiskSize.Units(vcfg.Byte)) {
			delta := vcfg.Bytes(size) - cfg.VM.DiskSize
			delta.Align(vcfg.MiB)
			return fmt.Errorf("specified disk size %s insufficient to contain disk contents", delta)
		} else {
			size = int64(cfg.VM.DiskSize.Units(vcfg.Byte))
		}
	}

	alignment := args.SizeAlign
	if alignment == 0 {
		alignment = 1
	}
	alignment = lcm(args.Format.Alignment(), alignment)
	if size%args.Format.Alignment() != 0 {
		size = (size/alignment + 1) * alignment
	}

	err = vimgBuilder.Prebuild(ctx, size)
	if err != nil {
		return err
	}

	err = args.Format.build(ctx, w, vimgBuilder, cfg)
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

func buildRAW(ctx context.Context, w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) error {
	err := b.Build(ctx, w)
	if err != nil {
		return err
	}
	return nil
}

func buildStreamOptimizedVMDK(ctx context.Context, w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) error {

	vw, err := vmdk.NewStreamOptimizedWriter(w, b)
	if err != nil {
		return err
	}
	defer vw.Close()

	err = b.Build(ctx, vw)
	if err != nil {
		return err
	}
	return nil

}

func buildSparseVMDK(ctx context.Context, w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) error {

	vw, err := vmdk.NewSparseWriter(w, b)
	if err != nil {
		return err
	}
	defer vw.Close()

	err = b.Build(ctx, vw)
	if err != nil {
		return err
	}
	return nil

}

func buildGCPArchive(ctx context.Context, w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) error {

	gw, err := gcparchive.NewWriter(w, b)
	if err != nil {
		return err
	}
	defer gw.Close()

	err = b.Build(ctx, gw)
	if err != nil {
		return err
	}
	return nil

}

func buildXVA(ctx context.Context, w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) error {

	gw, err := xva.NewWriter(w, b, cfg)
	if err != nil {
		return err
	}
	defer gw.Close()

	err = b.Build(ctx, gw)
	if err != nil {
		return err
	}
	return nil

}

func buildFixedVHD(ctx context.Context, w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) error {

	vw, err := vhd.NewFixedWriter(w, b)
	if err != nil {
		return err
	}
	defer vw.Close()

	err = b.Build(ctx, vw)
	if err != nil {
		return err
	}
	return nil

}

func buildDynamicVHD(ctx context.Context, w io.WriteSeeker, b *vimg.Builder, cfg *vcfg.VCFG) error {

	vw, err := vhd.NewDynamicWriter(w, b)
	if err != nil {
		return err
	}
	defer vw.Close()

	err = b.Build(ctx, vw)
	if err != nil {
		return err
	}
	return nil

}
