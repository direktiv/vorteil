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

func Stat(log elog.View, cmd *cobra.Command, args []string, flagOS bool) error {
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

	fpath := "/"
	if len(args) > 1 {
		fpath = args[1]
	}

	if flagOS {
		var s string
		var size int
		ftype := "regular file"

		fpath = strings.TrimPrefix(fpath, "/")
		if fpath == "" {
			s = "/"
			size = 0
		} else {
			kfiles, err := iio.KernelFiles()
			if err != nil {
				return err
			}

			for _, kf := range kfiles {
				if kf.Name == fpath {
					s = fpath
					size = kf.Size
					break
				}
			}

			if s == "" {
				return fmt.Errorf("kernel file not found: %s", fpath)
			}
		}

		log.Printf("File: %s\t%s", s, ftype)
		log.Printf("Size: %s", PrintableSize(size))
		log.Printf("Inode: -")
		log.Printf("Access: -")
		log.Printf("Uid: -")
		log.Printf("Gid: -")
		log.Printf("Access: -")
		log.Printf("Modify: -")
		log.Printf("Create: -")

	} else {
		ino, err := iio.ResolvePathToInodeNo(fpath)
		if err != nil {
			return err
		}

		inode, err := iio.ResolveInode(ino)
		if err != nil {
			return err
		}

		var ftype string

		var user, group string
		user = "?"
		group = "?"
		if inode.UID == vdecompiler.VorteilUserID {
			user = vdecompiler.VorteilUserName
		}
		if inode.GID == vdecompiler.VorteilGroupID {
			group = vdecompiler.VorteilGroupName
		}

		log.Printf("File: %s\t%s", filepath.Base(fpath), ftype)
		log.Printf("Size: %s", PrintableSize(inode.Fullsize()))
		// TODO: log.Printf("Blocks: %s", PrintableSize(int()))
		// TODO: log.Printf("IO Block: %s", PrintableSize())
		log.Printf("Inode: %d", ino)
		// TODO: log.Printf("Links: %s")
		log.Printf("Access: %#o/%s", inode.Mode&vdecompiler.InodePermissionsMask, inode.Permissions())
		log.Printf("Uid: %d (%s)", inode.UID, user)
		log.Printf("Gid: %d (%s)", inode.GID, group)
		log.Printf("Access: %s", time.Unix(int64(inode.LastAccessTime), 0))
		log.Printf("Modify: %s", time.Unix(int64(inode.ModificationTime), 0))
		log.Printf("Create: %s", time.Unix(int64(inode.CreationTime), 0))
	}
	return nil
}
