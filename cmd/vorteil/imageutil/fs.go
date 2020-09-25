package imageutil

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

func FSImg(cmd *cobra.Command, args []string) error {
	img := args[0]
	dst := args[1]

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()

	iio, err := vdecompiler.Open(img)
	if err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	defer iio.Close()

	rdr, err := iio.PartitionReader(vdecompiler.FilesystemPartitionName)
	if err != nil {
		_ = os.Remove(f.Name())
		return err
	}

	_, err = io.Copy(f, rdr)
	if err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return nil
}
func FS(log elog.View, cmd *cobra.Command, args []string) error {
	numbers, err := cmd.Flags().GetString("numbers")
	if err != nil {
		return err
	}

	err = SetNumbersMode(numbers)
	if err != nil {
		return fmt.Errorf("couldn't parse value of --numbers: %v", err)
	}

	img := args[0]

	iio, err := vdecompiler.Open(img)
	if err != nil {
		return err
	}
	defer iio.Close()

	entry, err := iio.GPTEntry(vdecompiler.FilesystemPartitionName)
	if err != nil {
		return err
	}

	sb, err := iio.Superblock(0)
	if err != nil {
		return err
	}

	log.Printf("First LBA:        \t%s", PrintableSize(int(entry.FirstLBA)))
	log.Printf("Last LBA:         \t%s", PrintableSize(int(entry.LastLBA)))
	log.Printf("Type:             \text2")

	blocksize := 1024 << int(sb.BlockSize)
	log.Printf("Block size:       \t%s", PrintableSize(blocksize))
	log.Printf("Blocks allocated: \t%s / %s", PrintableSize(int(sb.TotalBlocks-sb.UnallocatedBlocks)), PrintableSize(int(sb.TotalBlocks)))
	log.Printf("Inodes allocated: \t%s / %s", PrintableSize(int(sb.TotalInodes-sb.UnallocatedInodes)), PrintableSize(int(sb.TotalInodes)))

	log.Printf("Block groups:     \t%s", PrintableSize(int((sb.TotalBlocks+sb.BlocksPerGroup-1)/sb.BlocksPerGroup)))
	log.Printf("  Max blocks each:\t%s", PrintableSize(int(sb.BlocksPerGroup)))
	log.Printf("  Max inodes each:\t%s", PrintableSize(int(sb.InodesPerGroup)))

	// TODO: log.Printf("Expansion ceiling: %s")
	log.Printf("Last mount time:  \t%s", time.Unix(int64(sb.LastMountTime), 0))
	log.Printf("Last written time:\t%s", time.Unix(int64(sb.LastWrittenTime), 0))

	// TODO: files
	// TODO: dirs
	// TODO: free space
	return nil
}
