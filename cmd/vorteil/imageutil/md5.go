package imageutil

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"strings"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

func MD5(log elog.View, args []string, flagOS bool) error {
	img := args[0]

	iio, err := vdecompiler.Open(img)
	if err != nil {
		return err
	}
	defer iio.Close()

	fpath := args[1]
	var rdr io.Reader

	if flagOS {
		fpath = strings.TrimPrefix(fpath, "/")
		rdr, err = iio.KernelFile(fpath)
		if err != nil {
			return err
		}
	} else {
		ino, err := iio.ResolvePathToInodeNo(fpath)
		if err != nil {
			return err
		}

		inode, err := iio.ResolveInode(ino)
		if err != nil {
			return err
		}

		if inode.IsDirectory() {
			return err
		}

		rdr, err = iio.InodeReader(inode)
		if err != nil {
			return err
		}

		rdr = io.LimitReader(rdr, int64(inode.Fullsize()))
	}

	hasher := md5.New()
	_, err = io.Copy(hasher, rdr)
	if err != nil {
		return err
	}

	log.Printf("%s", hex.EncodeToString(hasher.Sum(nil)))
	return nil
}
