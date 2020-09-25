package imageutil

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

func CP(log elog.View, args []string, flagOS bool) error {

	img := args[0]

	iio, err := vdecompiler.Open(img)
	if err != nil {
		return err
	}
	defer iio.Close()

	dest := args[2]
	fi, err := os.Stat(dest)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var into bool
	if !os.IsNotExist(err) && fi.IsDir() {
		into = true
	}

	fpath := args[1]
	dpath := dest
	if into {
		dpath = filepath.Join(dest, filepath.Base(fpath))
	}

	if flagOS {
		if fpath != "" && fpath != "/" && fpath != "." {
			// single file
			fpath = strings.TrimPrefix(fpath, "/")
			r, err := iio.KernelFile(fpath)
			f, err := os.Create(dpath)
			defer f.Close()
			_, err = io.Copy(f, r)
			if err != nil {
				return err
			}
		} else {
			// entire folder
			kfiles, err := iio.KernelFiles()
			if err != nil {
				return err
			}

			err = os.MkdirAll(dpath, 077)
			if err != nil {
				return err
			}

			for _, kf := range kfiles {
				r, err := iio.KernelFile(kf.Name)
				if err != nil {
					return err
				}

				f, err := os.Create(filepath.Join(dpath, kf.Name))
				if err != nil {
					return err
				}
				defer f.Close()

				_, err = io.Copy(f, r)
				if err != nil {
					return err
				}

				err = f.Close()
				if err != nil {
					return err
				}
			}
		}
	}

	var recurse func(int, string, string) error
	recurse = func(ino int, rpath string, dpath string) error {

		inode, err := iio.ResolveInode(ino)
		if err != nil {
			return err
		}

		if inode.IsRegularFile() {
			f, err := os.Create(dpath)
			if err != nil {
				return err
			}
			defer f.Close()

			rdr, err := iio.InodeReader(inode)
			if err != nil {
				return err
			}

			_, err = io.CopyN(f, rdr, int64(inode.Fullsize()))
			if err != nil {
				return err
			}
			return nil
		}

		if inode.IsSymlink() {

			rdr, err := iio.InodeReader(inode)
			if err != nil {
				return err
			}
			data, err := ioutil.ReadAll(rdr)
			if err != nil {
				return err
			}
			err = os.Symlink(string(data), dpath)
			if err != nil {
				return err
			}
			return nil
		}

		if !inode.IsDirectory() {
			log.Warnf("skipping abnormal file: %s", rpath)
			return nil
		}

		err = os.MkdirAll(dpath, 0777)
		if err != nil {
			return err
		}

		entries, err := iio.Readdir(inode)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if entry.Name == "." || entry.Name == ".." {
				continue
			}
			err = recurse(entry.Inode, filepath.ToSlash(filepath.Join(rpath, entry.Name)), filepath.Join(dpath, entry.Name))
			if err != nil {
				return err
			}
		}

		return nil
	}

	ino, err := iio.ResolvePathToInodeNo(fpath)
	if err != nil {
		return err
	}
	err = recurse(ino, filepath.Base(fpath), dpath)
	if err != nil {
		return err
	}

	return nil
}
