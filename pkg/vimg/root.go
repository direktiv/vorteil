package vimg

import (
	"context"
	"io"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vio"
)

func (b *Builder) prebuildRoot(ctx context.Context) error {

	err := ctx.Err()
	if err != nil {
		return err
	}

	b.rootFirstLBA = b.osLastLBA + 1
	b.rootLastLBA = b.lastUsableLBA

	size := (b.rootLastLBA - b.rootFirstLBA + 1) * SectorSize

	err = b.fs.Precompile(ctx, size)
	if err != nil {
		return err
	}

	return nil
}

func (b *Builder) writeRoot(ctx context.Context, w io.WriteSeeker) error {

	_, err := w.Seek(b.rootFirstLBA*SectorSize, io.SeekStart)
	if err != nil {
		return err
	}

	ws, err := vio.WriteSeeker(w)
	if err != nil {
		return err
	}

	err = b.fs.Compile(ctx, ws)
	if err != nil {
		return err
	}

	return nil

}

func (b *Builder) rootRegionIsHole(begin, size int64) bool {
	return b.fs.RegionIsHole(begin, size)
}

func (b *Builder) validateRootArgs() error {

	// inject files/directories here
	for _, dir := range []string{"dev", "vorteil", "tmp", "proc", "sys"} {
		err := b.fs.Mkdir(dir)
		if err != nil {
			return err
		}
	}

	return nil
}

func (b *Builder) calculateMinimumRootSize(ctx context.Context) error {

	progress := b.log.NewProgress("Planning file-system", "", 0)
	defer progress.Finish(false)

	if b.vcfg.VM.Inodes == 0 {
		b.fs.SetMinimumInodesPer64MiB(1024)
	} else {
		b.fs.SetMinimumInodes(int64(b.vcfg.VM.Inodes))
	}

	// if the user runs with shell we add all busybox commands
	// and link them to /usr/bin and /bin so we need to increase inodes
	if b.kernelOptions.Shell {
		b.fs.IncreaseMinimumInodes(2000)
	}

	if b.vcfg.VM.DiskSize.IsDelta() {
		delta := vcfg.Bytes(0)
		delta.ApplyDelta(b.vcfg.VM.DiskSize)
		b.fs.IncreaseMinimumFreeSpace(int64(delta.Units(vcfg.Byte)))
	}

	err := b.fs.Commit(ctx)
	if err != nil {
		return err
	}

	b.minSize += b.fs.MinimumSize()
	progress.Finish(true)

	return nil
}
