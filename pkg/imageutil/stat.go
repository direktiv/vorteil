package imageutil

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// StatCMD prints detailed metdata relating to the file at FILE_PATH
var StatCMD = &cobra.Command{
	Use:   "stat IMAGE [FILEPATH]",
	Short: "Print detailed metadata relating to the file at FILE_PATH.",
	Args:  cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		flagOs, err := cmd.Flags().GetBool("vpartition")
		if err != nil {
			panic(err)
		}
		numbers, err := cmd.Flags().GetString("numbers")
		if err != nil {
			panic(err)
		}

		err = SetNumbersMode(numbers)
		if err != nil {
			log.Errorf("couldn't parse value of --numbers: %v", err)
			return
		}

		img := args[0]

		iio, err := vdecompiler.Open(img)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		fpath := "/"
		if len(args) > 1 {
			fpath = args[1]
		}

		if flagOs {
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
					log.Errorf("%v", err)
					os.Exit(1)
				}

				for _, kf := range kfiles {
					if kf.Name == fpath {
						s = fpath
						size = kf.Size
						break
					}
				}

				if s == "" {
					log.Errorf("kernel file not found: %s", fpath)
					os.Exit(1)
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
				log.Errorf("%v", err)
				os.Exit(1)
			}

			inode, err := iio.ResolveInode(ino)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
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
	},
}
