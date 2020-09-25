package imageutil

import (
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// CatCMD is a way to cat a file on a disk
var CatCMD = &cobra.Command{
	Use:   "cat IMAGE FILEPATH...",
	Short: "Concatenate files and print on the standard output.",
	Args:  cobra.MinimumNArgs(2),
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

		for i := 1; i < len(args); i++ {
			fpath := args[i]
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

				if !inode.IsRegularFile() {
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

			_, err := io.Copy(os.Stdout, rdr)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}

		}
	},
}
