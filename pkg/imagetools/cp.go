package imagetools

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

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

	return copyImageFileRecursive(vorteilImage, ino, filepath.Base(imageFilePath), destFilePath)
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

func copyImageFileRecursive(vorteilImage *vdecompiler.IO, ino int, rpath string, destFilePath string) error {
	var f *os.File
	var rdr io.Reader
	var err error
	var entries []*vdecompiler.DirectoryEntry
	inode, err := vorteilImage.ResolveInode(ino)
	if err != nil {
		return err
	}

	if vdecompiler.InodeIsRegularFile(inode) {
		if f, err = os.Create(destFilePath); err == nil {
			defer f.Close()
			if rdr, err = vorteilImage.InodeReader(inode); err == nil {
				_, err = io.CopyN(f, rdr, int64(vdecompiler.InodeSize(inode)))
			}
		}
		goto DONE
	}

	if vdecompiler.InodeIsSymlink(inode) {
		var data []byte

		if rdr, err = vorteilImage.InodeReader(inode); err == nil {
			if data, err = ioutil.ReadAll(rdr); err == nil {
				err = os.Symlink(string(data), destFilePath)
			}
		}
		goto DONE
	}

	if !vdecompiler.InodeIsDirectory(inode) {
		goto SKIP
	}

	err = os.MkdirAll(destFilePath, 0777)
	if err != nil {
		return err
	}

	entries, err = vorteilImage.Readdir(inode)
	if err == nil {
		for _, entry := range entries {
			if entry.Name == "." || entry.Name == ".." {
				continue
			}
			err = copyImageFileRecursive(vorteilImage, entry.Inode, filepath.ToSlash(filepath.Join(rpath, entry.Name)), filepath.Join(destFilePath, entry.Name))
			if err != nil {
				break
			}
		}
	}

DONE:
SKIP:
	return err

}
