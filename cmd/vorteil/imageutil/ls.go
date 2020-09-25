package imageutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

func LS(log elog.View, cmd *cobra.Command, args []string, flagOS bool) error {
	numbers, err := cmd.Flags().GetString("numbers")
	if err != nil {
		return err
	}

	err = SetNumbersMode(numbers)
	if err != nil {
		return fmt.Errorf("couldn't parse value of --numbers: %v", err)
	}

	var reiterating bool

	all, err := cmd.Flags().GetBool("all")
	if err != nil {
		return err
	}

	almostAll, err := cmd.Flags().GetBool("almost-all")
	if err != nil {
		return err
	}

	long, err := cmd.Flags().GetBool("long")
	if err != nil {
		return err
	}

	recursive, err := cmd.Flags().GetBool("recursive")
	if err != nil {
		return err
	}

	img := args[0]

	iio, err := vdecompiler.Open(img)
	if err != nil {
		return err
	}
	defer iio.Close()

	var fpaths []string
	var inos []int
	var inodes []*vdecompiler.Inode
	var table [][]string
	var entries []*vdecompiler.DirectoryEntry

	var fpath string
	if len(args) > 1 {
		fpath = args[1]
	} else {
		fpath = "/"
	}

	if flagOS {
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

	ino, err := iio.ResolvePathToInodeNo(fpath)
	if err != nil {
		return err
	}

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
				log.Errorf("%v", err)
				os.Exit(1)
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
