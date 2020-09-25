package imageutil

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// DU calculates file space usage.
func DU(log elog.View, cmd *cobra.Command, args []string) error {
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

	all, err := cmd.Flags().GetBool("all")
	if err != nil {
		return err
	}

	free, err := cmd.Flags().GetBool("free")
	if err != nil {
		return err
	}

	maxDepth, err := cmd.Flags().GetInt("max-depth")
	if err != nil {
		return err
	}

	var table [][]string
	table = [][]string{{"", ""}}

	var depth = 0

	var recurse func(*vdecompiler.Inode, string) (int, error)
	recurse = func(inode *vdecompiler.Inode, name string) (int, error) {

		depth++
		defer func() {
			depth--
		}()

		var size int
		size = int(inode.Sectors) * vdecompiler.SectorSize

		if !inode.IsDirectory() {
			return size, nil
		}

		entries, err := iio.Readdir(inode)
		if err != nil {
			return 0, err
		}

		var delta int
		for i := 2; i < len(entries); i++ {
			entry := entries[i]
			child := filepath.ToSlash(filepath.Join(name, entry.Name))

			cinode, err := iio.ResolveInode(entry.Inode)
			if err != nil {
				return 0, err
			}

			delta, err = recurse(cinode, child)
			if err != nil {
				return 0, err
			}
			if all || inode.IsDirectory() {
				if (maxDepth >= 0 && depth <= maxDepth) || maxDepth < 0 {
					table = append(table, []string{child, fmt.Sprintf("%s", PrintableSize(delta))})
				}
			}
			size += delta
		}

		return size, nil
	}

	var fpath string
	if len(args) > 1 {
		fpath = args[1]
	} else {
		fpath = "/"
	}

	ino, err := iio.ResolvePathToInodeNo(fpath)
	if err != nil {
		return err
	}

	inode, err := iio.ResolveInode(ino)
	if err != nil {
		return err
	}

	size, err := recurse(inode, fpath)
	if err != nil {
		return err
	}

	table = append(table, []string{fpath, fmt.Sprintf("%s", PrintableSize(size))})

	PlainTable(table)

	if free {
		sb, err := iio.Superblock(0)
		if err != nil {
			return err
		}

		leftover := int(sb.UnallocatedBlocks) * int(1024<<sb.BlockSize)
		log.Printf("Free space: %s", PrintableSize(leftover))
	}
	return nil
}
