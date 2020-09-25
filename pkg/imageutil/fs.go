package imageutil

import (
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// FsCMD summarizes the information in the main file-system's metdata.
var FsCMD = &cobra.Command{
	Use:   "fs IMAGE",
	Short: "Summarize the information in the main file-system's metadata.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		numbers, err := cmd.Flags().GetString("numbers")
		if err != nil {
			panic(err)
		}

		err = SetNumbersMode(numbers)
		if err != nil {
			log.Errorf("couldn't parse value of --numbers: %v", err)
			os.Exit(1)
		}

		img := args[0]

		iio, err := vdecompiler.Open(img)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		entry, err := iio.GPTEntry(vdecompiler.FilesystemPartitionName)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		sb, err := iio.Superblock(0)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
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
	},
}

// FsImgCMD copies the image's file-system partition
var FsImgCMD = &cobra.Command{
	Use:   "fsimg IMAGE DEST",
	Short: "Copy the image's file-system partition.",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		img := args[0]
		dst := args[1]

		f, err := os.Create(dst)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer f.Close()

		iio, err := vdecompiler.Open(img)
		if err != nil {
			_ = os.Remove(f.Name())
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		rdr, err := iio.PartitionReader(vdecompiler.FilesystemPartitionName)
		if err != nil {
			_ = os.Remove(f.Name())
			log.Errorf("%v", err)
			os.Exit(1)
		}

		_, err = io.Copy(f, rdr)
		if err != nil {
			_ = os.Remove(f.Name())
			log.Errorf("%v", err)
			os.Exit(1)
		}
	},
}
