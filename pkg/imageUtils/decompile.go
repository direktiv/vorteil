package imageUtils

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// DecompileReport ...
type DecompileReport struct {
	CopyTouched bool
	ImageFiles  []DecompiledFile
}

// DecompiledFile ...
type DecompiledFile struct {
	Path   string
	Copied bool
	Result CopyResult
}

// CopyResult ...
type CopyResult int

const (
	SkippedNotTouched   CopyResult = 0
	SkippedAbnormalFile            = 1
	CopiedRegularFile              = 2
	CopiedSymlink                  = 3
	CopiedMkDir                    = 4
)

func createSymlinkCallback(iio *vdecompiler.IO, inode *vdecompiler.Inode, dpath string) func() error {
	return func() error {
		rdr, err := iio.InodeReader(inode)
		if err != nil {
			return err
		}
		data, err := ioutil.ReadAll(rdr)
		if err != nil {
			return err
		}

		err = os.Symlink(string(string(data)), dpath)
		if err != nil {
			return err
		}
		return nil
	}
}

func copyInodeToRegularFile(iio *vdecompiler.IO, inode *vdecompiler.Inode, dpath string) error {
	var err error
	var f *os.File
	var rdr io.Reader

	err = utilFileNotExists(dpath)
	if err != nil {
		return err
	}

	f, err = os.Create(dpath)
	if err != nil {
		return err
	}
	defer f.Close()

	rdr, err = iio.InodeReader(inode)
	if err != nil {
		return err
	}

	_, err = io.CopyN(f, rdr, int64(inode.Fullsize()))
	return err
}

func utilFileNotExists(fpath string) error {
	_, err := os.Stat(fpath)
	if !os.IsNotExist(err) {
		if err == nil {
			err = fmt.Errorf("file already exists: %s", fpath)
		}
		return err
	}
	return nil
}

// DecompileImage ...
func DecompileImage(vorteilImagePath string, Outputpath string, copyTouched bool) (DecompileReport, error) {
	report := DecompileReport{
		ImageFiles:  make([]DecompiledFile, 0),
		CopyTouched: copyTouched,
	}

	iio, err := vdecompiler.Open(vorteilImagePath)
	if err != nil {
		return report, err
	}
	defer iio.Close()

	fi, err := os.Stat(Outputpath)
	if err != nil && !os.IsNotExist(err) {
		return report, err
	}
	var into bool
	if !os.IsNotExist(err) && fi.IsDir() {
		into = true
	}

	fpath := "/"
	dpath := Outputpath
	if into {
		dpath = filepath.ToSlash(filepath.Join(Outputpath, filepath.Base(fpath)))
	}

	var counter int

	symlinkCallbacks := make([]func() error, 0)

	var recurse func(int, string, string) error
	recurse = func(ino int, rpath string, dpath string) error {
		var entries []*vdecompiler.DirectoryEntry

		inode, err := iio.ResolveInode(ino)
		if err != nil {
			return err
		}

		if copyTouched && inode.LastAccessTime == 0 && !inode.IsDirectory() && rpath != "/" {
			report.ImageFiles = append(report.ImageFiles, DecompiledFile{
				Copied: false,
				Path:   rpath,
				Result: SkippedNotTouched,
			})
			goto DONE
		}

		counter++

		if inode.IsRegularFile() {
			err = copyInodeToRegularFile(iio, inode, dpath)
			if err == nil {
				report.ImageFiles = append(report.ImageFiles, DecompiledFile{
					Copied: false,
					Path:   rpath,
					Result: CopiedRegularFile,
				})
			}
			goto DONE
		}

		if inode.IsSymlink() {
			symlinkCallbacks = append(symlinkCallbacks, createSymlinkCallback(iio, inode, dpath))
			report.ImageFiles = append(report.ImageFiles, DecompiledFile{
				Copied: false,
				Path:   rpath,
				Result: CopiedSymlink,
			})
			goto DONE
		}

		if !inode.IsDirectory() {
			report.ImageFiles = append(report.ImageFiles, DecompiledFile{
				Copied: false,
				Path:   rpath,
				Result: SkippedAbnormalFile,
			})
			goto DONE
		}

		// INODE IS DIR
		err = utilFileNotExists(dpath)
		if err == nil {
			err = os.MkdirAll(dpath, 0777)
			if err == nil {
				report.ImageFiles = append(report.ImageFiles, DecompiledFile{
					Copied: false,
					Path:   rpath,
					Result: CopiedMkDir,
				})
				entries, err = iio.Readdir(inode)
			}
		}

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

	DONE:
		return err
	}

	ino, err := iio.ResolvePathToInodeNo(fpath)
	if err != nil {
		return report, err
	}
	err = recurse(ino, filepath.ToSlash(filepath.Base(fpath)), dpath)
	if err != nil {
		return report, err
	}

	for _, fn := range symlinkCallbacks {
		err = fn()
		if err != nil {
			break
		}
	}

	return report, err
}
