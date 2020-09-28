package imageUtils

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/prometheus/common/log"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// CopyImageFile ...
func CopyImageFile(vorteilImage *vdecompiler.IO, imageFilePath string, destFilePath string, seekOS bool) error {
	fi, err := os.Stat(destFilePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var into bool
	if !os.IsNotExist(err) && fi.IsDir() {
		into = true
	}

	if into {
		destFilePath = filepath.Join(destFilePath, filepath.Base(imageFilePath))
	}

	if seekOS {
		return copyImageFileFromVPartition(vorteilImage, imageFilePath, destFilePath)
	}

	ino, err := vorteilImage.ResolvePathToInodeNo(imageFilePath)
	if err != nil {
		return err
	}
	err = copyImageFileRecusive(vorteilImage, ino, filepath.Base(imageFilePath), destFilePath)
	if err != nil {
		return err
	}

	return nil
}

func copyImageFileFromVPartition(vorteilImage *vdecompiler.IO, imageFilePath string, destFilePath string) error {
	var err error
	var r io.Reader
	var f *os.File
	if imageFilePath != "" && imageFilePath != "/" && imageFilePath != "." {
		// single file
		imageFilePath = strings.TrimPrefix(imageFilePath, "/")
		r, err := vorteilImage.KernelFile(imageFilePath)
		if err == nil {
			if f, err := os.Create(destFilePath); err == nil {
				defer f.Close()
				_, err = io.Copy(f, r)
			}
		}
	} else {
		// entire folder
		kfiles, err := vorteilImage.KernelFiles()
		if err != nil {
			return err
		}

		err = os.MkdirAll(destFilePath, 077)
		if err != nil {
			return err
		}

		for _, kf := range kfiles {
			r, err = vorteilImage.KernelFile(kf.Name)
			if err != nil {
				break
			}

			f, err = os.Create(filepath.Join(destFilePath, kf.Name))
			if err != nil {
				break
			}
			defer f.Close()

			_, err = io.Copy(f, r)
			if err != nil {
				break
			}

			err = f.Close()
			if err != nil {
				break
			}
		}
	}
	return err
}

func copyImageFileRecusive(vorteilImage *vdecompiler.IO, ino int, rpath string, destFilePath string) error {
	inode, err := vorteilImage.ResolveInode(ino)
	if err != nil {
		return err
	}

	if inode.IsRegularFile() {
		f, err := os.Create(destFilePath)
		if err != nil {
			return err
		}
		defer f.Close()

		rdr, err := vorteilImage.InodeReader(inode)
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

		rdr, err := vorteilImage.InodeReader(inode)
		if err != nil {
			return err
		}
		data, err := ioutil.ReadAll(rdr)
		if err != nil {
			return err
		}
		err = os.Symlink(string(data), destFilePath)
		if err != nil {
			return err
		}
		return nil
	}

	if !inode.IsDirectory() {
		log.Warnf("skipping abnormal file: %s", rpath)
		return nil
	}

	err = os.MkdirAll(destFilePath, 0777)
	if err != nil {
		return err
	}

	entries, err := vorteilImage.Readdir(inode)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.Name == "." || entry.Name == ".." {
			continue
		}
		err = copyImageFileRecusive(vorteilImage, entry.Inode, filepath.ToSlash(filepath.Join(rpath, entry.Name)), filepath.Join(destFilePath, entry.Name))
		if err != nil {
			return err
		}
	}

	return nil

}
