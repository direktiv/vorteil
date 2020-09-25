package imageutil

import (
	"fmt"
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

func printVPartition(log elog.View, iio *vdecompiler.IO, fpath string, long bool) error {
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

func readLS(log elog.View, fpath string, img string, long bool, flagOS bool) (*vdecompiler.IO, int, error) {
	iio, err := vdecompiler.Open(img)
	if err != nil {
		return nil, 0, err
	}

	if flagOS {
		err = printVPartition(log, iio, fpath, long)
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

func parseLSFlags(cmd *cobra.Command) (bool, bool, bool, bool, error) {
	err := lsSetNumbers(cmd)
	if err != nil {
		return false, false, false, false, err
	}

	all, almostAll, long, recursive, err := readLSBoolFlags(cmd)
	if err != nil {
		return false, false, false, false, err
	}
	return all, almostAll, long, recursive, err
}

// LS lists directory contents.
func LS(log elog.View, cmd *cobra.Command, args []string, flagOS bool) error {

	all, almostAll, long, recursive, err := parseLSFlags(cmd)
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
	iio, ino, err := readLS(log, fpath, img, long, flagOS)
	if err != nil {
		return err
	}
	defer iio.Close()
	lsReader := lsReader{log: log, iio: iio, ino: ino, long: long, recursive: recursive, all: all, almostAll: almostAll, fpath: fpath}

	err = readInodesLS(&lsReader)
	if err != nil {
		return err
	}

	return nil
}

type lsReader struct {
	log       elog.View
	iio       *vdecompiler.IO
	ino       int
	long      bool
	recursive bool
	all       bool
	almostAll bool
	fpath     string
}

func readInodesLS(read *lsReader) error {
	var reiterating bool
	var fpaths []string
	var inos []int
	var inodes []*vdecompiler.Inode
	var table [][]string
	var entries []*vdecompiler.DirectoryEntry

inoEntry:
	inode, err := read.iio.ResolveInode(read.ino)
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
		read.log.Infof("")
	}

	entries, err = read.iio.Readdir(inode)
	if err != nil {
		return err
	}

	if read.recursive {
		read.log.Printf("%s:", read.fpath)
	}

	if read.long {
		table = [][]string{{"", "", "", "", "", "", ""}}
	}

	for _, entry := range entries {
		if !(read.all || read.almostAll) && strings.HasPrefix(entry.Name, ".") {
			continue
		}
		if read.almostAll && (entry.Name == "." || entry.Name == "..") {
			continue
		}

		if read.recursive && !(entry.Name == "." || entry.Name == "..") {
			fpaths = append(fpaths, filepath.ToSlash(filepath.Join(read.fpath, entry.Name)))
		}

		if read.long {
			child, err := read.iio.ResolveInode(entry.Inode)
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

			if read.recursive && !(entry.Name == "." || entry.Name == "..") {
				inodes = append(inodes, child)
			}
		} else {
			if read.recursive {
				read.log.Printf("  %s", entry.Name)
				if !(entry.Name == "." || entry.Name == "..") {
					inos = append(inos, entry.Inode)
				}
			} else {
				read.log.Printf("%s", entry.Name)
			}
		}
	}
	if read.long {
		PlainTable(table)
	}

skip:
	if read.recursive {
		reiterating = true
		if len(fpaths) > 0 {
			read.fpath = fpaths[0]
			fpaths = fpaths[1:]
		}
		if len(inos) > 0 {
			read.ino = inos[0]
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
