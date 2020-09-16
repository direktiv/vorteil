package vimg

import (
	"context"
	"errors"
	"io"
	"math/rand"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vkern"
)

type FSCompiler interface {
	Mkdir(path string) error
	AddFile(path string, r io.ReadCloser, size int64, force bool) error
	IncreaseMinimumFreeSpace(space int64)
	SetMinimumInodes(inodes int64)
	IncreaseMinimumInodes(inodes int64)
	Commit(ctx context.Context) error
	MinimumSize() int64
	Precompile(ctx context.Context, size int64) error
	Compile(ctx context.Context, w io.WriteSeeker) error
	RegionIsHole(begin, size int64) bool
}

type KernelOptions struct {
	Shell bool
}

type BuilderArgs struct {
	Seed       int64
	Kernel     KernelOptions
	FSCompiler FSCompiler
	VCFG       *vcfg.VCFG
}

type Builder struct {

	// The following variables need to be calculated in the NewBuilder step.
	rng           io.Reader
	minSize       int64
	fs            FSCompiler
	kernelOptions KernelOptions
	vcfg          *vcfg.VCFG
	kernel        vkern.CalVer
	kernelTags    []string
	linuxArgs     string
	defaultMTU    uint

	// The following variables need to be calculated in the prebuild step.
	size                      int64
	secondaryGPTHeaderLBA     int64
	secondaryGPTHeaderOffset  int64
	secondaryGPTEntriesLBA    int64
	secondaryGPTEntriesOffset int64
	configFirstLBA            int64
	osFirstLBA                int64
	osLastLBA                 int64
	rootFirstLBA              int64
	rootLastLBA               int64
	lastUsableLBA             int64
	gptEntries                []byte
	gptEntriesCRC             uint32
	diskUID                   []byte

	kernelBundle *vkern.ManagedBundle
	configData   []byte
}

func NewBuilder(ctx context.Context, args *BuilderArgs) (*Builder, error) {

	err := ctx.Err()
	if err != nil {
		return nil, err
	}

	// TODO: build logs

	b := new(Builder)
	b.rng = rand.New(rand.NewSource(args.Seed))
	b.fs = args.FSCompiler
	b.vcfg = args.VCFG
	b.kernelOptions = args.Kernel
	b.defaultMTU = 1500

	err = b.validateArgs(ctx)
	if err != nil {
		return nil, err
	}

	err = b.calculateMinimumSize(ctx)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (b *Builder) SetDefaultMTU(mtu uint) {
	b.defaultMTU = mtu
}

func (b *Builder) validateArgs(ctx context.Context) error {

	err := b.validateOSArgs(ctx)
	if err != nil {
		return err
	}

	err = b.validateRootArgs()
	if err != nil {
		return err
	}

	return nil
}

func (b *Builder) calculateMinimumSize(ctx context.Context) error {

	b.minSize = (3 + 2*GPTEntriesSectors) * SectorSize

	err := b.calculateMinimumOSPartitionSize(ctx)
	if err != nil {
		return err
	}

	err = b.calculateMinimumRootSize(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (b *Builder) Close() error {

	if b.kernelBundle != nil {
		err := b.kernelBundle.Close()
		if err != nil {
			return err
		}
	}

	return nil

}

func (b *Builder) MinimumSize() int64 {
	return b.minSize
}

func (b *Builder) Prebuild(ctx context.Context, size int64) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	b.size = size

	if size%SectorSize != 0 {
		return errors.New("image size must be a multiple of the sector size")
	}

	sectors := size / SectorSize
	b.secondaryGPTHeaderLBA = sectors - 1
	b.secondaryGPTHeaderOffset = b.secondaryGPTHeaderLBA * SectorSize
	b.secondaryGPTEntriesLBA = b.secondaryGPTHeaderLBA - GPTEntriesSectors
	b.secondaryGPTEntriesOffset = b.secondaryGPTEntriesLBA * SectorSize
	b.lastUsableLBA = b.secondaryGPTEntriesLBA - 1

	err = b.prebuildOS(ctx)
	if err != nil {
		return err
	}

	err = b.prebuildRoot(ctx)
	if err != nil {
		return err
	}

	// Generate the GPT entries here because it shows up twice and we need to
	// checksum it before we can write the first GPT header to avoid
	// backtracking when writing.
	err = b.generateGPTEntries()
	if err != nil {
		return err
	}

	return nil
}

func (b *Builder) Build(ctx context.Context, w io.WriteSeeker) error {

	err := b.writePartitions(ctx, w)
	if err != nil {
		return err
	}

	return nil

}

func (b *Builder) Size() int64 {
	return b.size
}

func (b *Builder) RegionIsHole(begin, size int64) bool {

	first := begin / SectorSize
	last := (begin + size - 1) / SectorSize

	if first >= b.rootFirstLBA && last <= b.rootLastLBA {
		// file-system holes
		pBegin := (first - b.rootFirstLBA) * SectorSize
		pSize := (last - first + 1) * SectorSize
		return b.rootRegionIsHole(pBegin, pSize)
	}

	if first >= b.osFirstLBA && last <= b.osLastLBA {
		// OS partition holes
		pBegin := (first - b.osLastLBA) * SectorSize
		pSize := (last - first + 1) * SectorSize
		return b.osRegionIsHole(pBegin, pSize)
	}

	if last < P0FirstLBA && first >= PrimaryGPTEntriesLBA+1 {
		return true // in the empty space of the primary GPT entries
	}

	if first >= b.secondaryGPTEntriesLBA+1 && last < b.secondaryGPTHeaderLBA {
		return true // in the empty space of the secondary GPT entries
	}

	return false

}
