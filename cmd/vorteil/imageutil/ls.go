package imageutil

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

func lsSetNumbers(cmd *cobra.Command) error {
	numbers, err := cmd.Flags().GetString("numbers")
	if err != nil {
		return err
	}

	err = SetNumbersMode(numbers)
	if err != nil {
		return fmt.Errorf("couldn't parse value of --numbers: %v", err)
	}
	return nil
}

func printVPartition(iio *vdecompiler.IO, fpath string, long bool) error {
	if fpath != "/" && fpath != "" && fpath != "." {
		return fmt.Errorf("bad FILE_PATH for vorteil partition: %s", fpath)
	}

	kfiles, err := iio.KernelFiles()
	if err != nil {
		return err
	}

	if long {
		var table [][]string
		table = [][]string{{"", "", "", "", "", "", ""}}
		for _, kf := range kfiles {
			table = append(table, []string{"----------", "?", "-", "-", "-", fmt.Sprintf("%s", PrintableSize(kf.Size)), kf.Name})
		}
		PlainTable(table)
	} else {
		for _, kf := range kfiles {
			log.Printf("%s", kf.Name)
		}
	}
	return nil
}

func readLSBoolFlags(cmd *cobra.Command) (bool, bool, bool, bool, error) {
	var returnErr error
	all, err := cmd.Flags().GetBool("all")
	if err != nil {
		returnErr = err
	}

	almostAll, err := cmd.Flags().GetBool("almost-all")
	if err != nil {
		returnErr = err
	}

	long, err := cmd.Flags().GetBool("long")
	if err != nil {
		returnErr = err
	}

	recursive, err := cmd.Flags().GetBool("recursive")
	if err != nil {
		returnErr = err
	}
	return all, almostAll, long, recursive, returnErr
}

func readLS(fpath string, img string, long bool, flagOS bool) (*vdecompiler.IO, int, error) {
	iio, err := vdecompiler.Open(img)
	if err != nil {
		return nil, 0, err
	}

	if flagOS {
		err = printVPartition(iio, fpath, long)
		if err != nil {
			return nil, 0, err
		}
	}

	ino, err := iio.ResolvePathToInodeNo(fpath)
	if err != nil {
		return nil, 0, err
	}
	return iio, ino, nil
}

// LS lists directory contents.
func LS(log elog.View, cmd *cobra.Command, args []string, flagOS bool) error {

	err := lsSetNumbers(cmd)
	if err != nil {
		return err
	}

	all, almostAll, long, recursive, err := readLSBoolFlags(cmd)
	if err != nil {
		return err
	}
	var fpath string
	if len(args) > 1 {
		fpath = args[1]
	} else {
		fpath = "/"
	}
	img := args[0]

	iio, ino, err := readLS(fpath, img, long, flagOS)
	if err != nil {
		return err
	}
	defer iio.Close()

	err = readInodesLS(log, iio, ino, long, recursive, all, almostAll, fpath)
	if err != nil {
		return err
	}

	return nil
}

func readInodesLS(log elog.View, iio *vdecompiler.IO, ino int, long, recursive, all, almostAll bool, fpath string) error {
	var reiterating bool

	var fpaths []string
	var inos []int
	var inodes []*vdecompiler.Inode
	var table [][]string
	var entries []*vdecompiler.DirectoryEntry
inoEntry:
	inode, err := iio.ResolveInode(ino)
	if err != nil {
		return err
	}

inodeEntry:
	if !inode.IsDirectory() {
		if reiterating {
			goto skip
		}

		// TODO: log.Info info about files
		return nil
	}

	if reiterating {
		log.Infof("")
	}

	entries, err = iio.Readdir(inode)
	if err != nil {
		return err
	}

	if recursive {
		log.Printf("%s:", fpath)
	}

	if long {
		table = [][]string{{"", "", "", "", "", "", ""}}
	}

	for _, entry := range entries {
		if !(all || almostAll) && strings.HasPrefix(entry.Name, ".") {
			continue
		}
		if almostAll && (entry.Name == "." || entry.Name == "..") {
			continue
		}

		if recursive && !(entry.Name == "." || entry.Name == "..") {
			fpaths = append(fpaths, filepath.ToSlash(filepath.Join(fpath, entry.Name)))
		}

		if long {
			child, err := iio.ResolveInode(entry.Inode)
			if err != nil {
				return err
			}
			links := "?"

			var uid, gid string
			if child.UID == vdecompiler.VorteilUserID {
				uid = vdecompiler.VorteilUserName
			} else {
				uid = fmt.Sprintf("%d", child.UID)
			}

			if child.GID == vdecompiler.VorteilGroupID {
				gid = vdecompiler.VorteilGroupName
			} else {
				gid = fmt.Sprintf("%d", child.GID)
			}

			ts := fmt.Sprintf("%s", time.Unix(int64(child.ModificationTime), 0))
			size := fmt.Sprintf("%s", PrintableSize(child.Fullsize()))

			table = append(table, []string{child.Permissions(), links, uid, gid, ts, size, entry.Name})

			if recursive && !(entry.Name == "." || entry.Name == "..") {
				inodes = append(inodes, child)
			}
		} else {

			if recursive {
				log.Printf("  %s", entry.Name)
				if !(entry.Name == "." || entry.Name == "..") {
					inos = append(inos, entry.Inode)
				}
			} else {
				log.Printf("%s", entry.Name)
			}
		}

	}

	if long {
		PlainTable(table)
	}

skip:
	if recursive {
		reiterating = true
		if len(fpaths) > 0 {
			fpath = fpaths[0]
			fpaths = fpaths[1:]
		}
		if len(inos) > 0 {
			ino = inos[0]
			inos = inos[1:]
			goto inoEntry
		}
		if len(inodes) > 0 {
			inode = inodes[0]
			inodes = inodes[1:]
			goto inodeEntry
		}
	}
	return nil
}