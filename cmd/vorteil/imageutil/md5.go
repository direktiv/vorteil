package imageutil

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// Md5CMD computes a MD5 checksum for a file on a vdecompiler
var Md5CMD = &cobra.Command{
	Use:   "md5 IMAGE FILEPATH",
	Short: "Compute MD5 checksum for a file on a vdecompiler.",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		flagOs, err := cmd.Flags().GetBool("vpartition")
		if err != nil {
			panic(err)
		}
		img := args[0]

		iio, err := vdecompiler.Open(img)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		fpath := args[1]
		var rdr io.Reader

		if flagOs {
			fpath = strings.TrimPrefix(fpath, "/")
			rdr, err = iio.KernelFile(fpath)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}
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

			if inode.IsDirectory() {
				log.Errorf("\"%s\" is not a regular file", fpath)
				os.Exit(1)
			}

			rdr, err = iio.InodeReader(inode)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}

			rdr = io.LimitReader(rdr, int64(inode.Fullsize()))
		}

		hasher := md5.New()
		_, err = io.Copy(hasher, rdr)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		log.Printf("%s", hex.EncodeToString(hasher.Sum(nil)))
	},
}
