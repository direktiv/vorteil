package vimg

import (
	"context"
	"errors"
	"io"
	"math/rand"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vkern"
)

// FSCompiler is an interface that must be satisfied for a file-system compipler
// to be usable in this logic.
type FSCompiler interface {
	Mkdir(path string) error
	AddFile(path string, r io.ReadCloser, size int64, force bool) error
	IncreaseMinimumFreeSpace(space int64)
	SetMinimumInodes(inodes int64)
	SetMinimumInodesPer64MiB(inodes int64)
	IncreaseMinimumInodes(inodes int64)
	Commit(ctx context.Context) error
	MinimumSize() int64
	Precompile(ctx context.Context, size int64) error
	Compile(ctx context.Context, w io.WriteSeeker) error
	RegionIsHole(begin, size int64) bool
}

// KernelOptions for settings that change kernel behaviour.
type KernelOptions struct {
	Shell bool
}

// BuilderArgs collects all of the arguments needed to call NewBuilder into one place.
type BuilderArgs struct {
	Seed       int64
	Kernel     KernelOptions
	FSCompiler FSCompiler
	VCFG       *vcfg.VCFG
	Logger     elog.View
}

// Builder is used for building a raw Vorteil image. Building happens in several
// stages to make the logic play nicely with potential external logic that needs
// some back-and-forth control over the build process.
type Builder struct {

	// The following variables need to be calculated in the NewBuilder step.
	log           elog.View
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

// NewBuilder returns a new Builder object configured according to the provided
// args. In this state it can calculate the minimum possible image size and
// accept some minor further configuration. Once this configuration is complete
// you should call Prebuild on it to proceed.
func NewBuilder(ctx context.Context, args *BuilderArgs) (*Builder, error) {

	err := ctx.Err()
	if err != nil {
		return nil, err
	}

	b := new(Builder)
	b.rng = rand.New(rand.NewSource(args.Seed))
	b.fs = args.FSCompiler
	b.vcfg = args.VCFG
	b.kernelOptions = args.Kernel
	b.defaultMTU = 1500
	b.log = args.Logger

	progress := b.log.NewProgress("Scanning inputs", "", 0)
	defer progress.Finish(false)

	err = b.validateArgs(ctx)
	if err != nil {
		return nil, err
	}

	err = b.calculateMinimumSize(ctx)
	if err != nil {
		return nil, err
	}

	progress.Finish(true)

	return b, nil
}

// SetDefaultMTU can be called before calling Prebuild in order to change the
// default MTU that will be applied to each NIC configuration (if not
// explicitly set in the VCFG).
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

// Close frees up any resources kept open by the Builder.
func (b *Builder) Close() error {

	if b.kernelBundle != nil {
		err := b.kernelBundle.Close()
		if err != nil {
			return err
		}
	}

	return nil

}

// KernelUsed returns the calver the disk was built with
func (b *Builder) KernelUsed() vkern.CalVer {
	return b.kernel
}

// MinimumSize returns the minimum number of bytes that are needed to build the
// image.
func (b *Builder) MinimumSize() int64 {
	return b.minSize
}

// Prebuild locks in a final raw image size (in bytes) and performs some
// preflight calculations to determine the final disk layout and to make the
// RegionIsHole function usable by external logic that wants to wrap the image
// in some sort of sparse virtual disk image format. Prebuild must be called
// before calling Build.
func (b *Builder) Prebuild(ctx context.Context, size int64) error {

	b.size = size

	if size%SectorSize != 0 {
		panic(errors.New("image size must be a multiple of the sector size"))
	}

	sectors := size / SectorSize
	b.secondaryGPTHeaderLBA = sectors - 1
	b.secondaryGPTHeaderOffset = b.secondaryGPTHeaderLBA * SectorSize
	b.secondaryGPTEntriesLBA = b.secondaryGPTHeaderLBA - GPTEntriesSectors
	b.secondaryGPTEntriesOffset = b.secondaryGPTEntriesLBA * SectorSize
	b.lastUsableLBA = b.secondaryGPTEntriesLBA - 1

	err := b.prebuildOS(ctx)
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

// Build is the final operation performed by the Builder, and should only be
// called after a successful call to the Prebuild function. It writes the
// file-system to the provided io.WriteSeeker, w. Despite using io.Seeker
// functionality to improve performance, the Builder has been written in a way
// such that it never needs to seek "backwards", which means you can wrap any
// io.Writer with a vio.WriteSeeker and it will work.
func (b *Builder) Build(ctx context.Context, w io.WriteSeeker) error {

	progress := b.log.NewProgress("Writing image", "KiB", b.size)
	defer progress.Finish(false)

	err := b.writePartitions(ctx, elog.MultiWriteSeeker(w, progress))
	if err != nil {
		return err
	}

	progress.Finish(true)
	return nil

}

// Size returns the full final size of the raw disk image.
func (b *Builder) Size() int64 {
	return b.size
}

func (b *Builder) isGPTHole(first, last int64) bool {

	if last < P0FirstLBA && first >= PrimaryGPTEntriesLBA+1 {
		return true // in the empty space of the primary GPT entries
	}

	if first >= b.secondaryGPTEntriesLBA+1 && last < b.secondaryGPTHeaderLBA {
		return true // in the empty space of the secondary GPT entries
	}

	return false

}

// RegionIsHole can be called after a successful Prebuild. Its purpose is to
// provide advance notice to sparse disk image formatting logic on regions
// within the image that will be completely empty. The two args are measured in
// bytes, and the function returns true if every byte starting at begin and
// continuing for the full size is zeroed.
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

	if b.isGPTHole(first, last) {
		return true
	}

	return false

}
