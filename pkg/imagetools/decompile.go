package imagetools

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/vorteil/vorteil/pkg/ext"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// DecompileReport : Info on the results of a Decompile Operation
type DecompileReport struct {
	SkipNotTouched bool
	ImageFiles     []DecompiledFile
}

// DecompiledFile holds the path of the decompiled file, and its results
type DecompiledFile struct {
	Path   string
	Result CopyResult
}

// CopyResult : Enum const for the results of a decompiled file
type CopyResult int

const (
	// SkippedNotTouched : File was skipped because it was not touched during runtime
	SkippedNotTouched CopyResult = 0
	// SkippedAbnormalFile : File was skipped because it was not a dir, file or symlink
	SkippedAbnormalFile = 1
	// CopiedRegularFile : File was regular and copied during decompile
	CopiedRegularFile = 2
	// CopiedSymlink : File was a symlink, and was reconstructed during decompile
	CopiedSymlink = 3
	// CopiedMkDir : File was a dir, and was reconstructed during decompile
	CopiedMkDir = 4
)

func createSymlinkCallback(vorteilImage *vdecompiler.IO, inode *ext.Inode, dpath string) func() error {
	return func() error {
		rdr, err := vorteilImage.InodeReader(inode)
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

func copyInodeToRegularFile(vorteilImage *vdecompiler.IO, inode *ext.Inode, dpath string) error {
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

	rdr, err = vorteilImage.InodeReader(inode)
	if err != nil {
		return err
	}

	_, err = io.CopyN(f, rdr, int64(vdecompiler.InodeSize(inode)))
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

// decompileImageRecursive : Recursively loop through all image nodes and decompile them to the correct files
func decompileImageRecursive(vorteilImage *vdecompiler.IO, report DecompileReport, symlinkCallbacks []func() error, ino int, rpath string, dpath string) (DecompileReport, []func() error, error) {
	var entries []*vdecompiler.DirectoryEntry

	inode, err := vorteilImage.ResolveInode(ino)
	if err != nil {
		return report, nil, err
	}

	if report.SkipNotTouched && inode.LastAccessTime == 0 && !vdecompiler.InodeIsDirectory(inode) && rpath != "/" {
		report.ImageFiles = append(report.ImageFiles, DecompiledFile{
			Path:   rpath,
			Result: SkippedNotTouched,
		})
		goto DONE
	}

	if vdecompiler.InodeIsSymlink(inode) {
		symlinkCallbacks = append(symlinkCallbacks, createSymlinkCallback(vorteilImage, inode, dpath))
		report.ImageFiles = append(report.ImageFiles, DecompiledFile{
			Path:   rpath,
			Result: CopiedSymlink,
		})
		goto DONE
	}

	if vdecompiler.InodeIsRegularFile(inode) {
		err = copyInodeToRegularFile(vorteilImage, inode, dpath)
		if err == nil {
			report.ImageFiles = append(report.ImageFiles, DecompiledFile{
				Path:   rpath,
				Result: CopiedRegularFile,
			})
		}
		goto DONE
	}

	if !vdecompiler.InodeIsDirectory(inode) {
		report.ImageFiles = append(report.ImageFiles, DecompiledFile{
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
				Path:   rpath,
				Result: CopiedMkDir,
			})
			entries, err = vorteilImage.Readdir(inode)
		}
	}

	if err != nil {
		return report, nil, err
	}

	for _, entry := range entries {
		if entry.Name == "." || entry.Name == ".." {
			continue
		}
		report, symlinkCallbacks, err = decompileImageRecursive(vorteilImage, report, symlinkCallbacks, entry.Inode, filepath.ToSlash(filepath.Join(rpath, entry.Name)), filepath.Join(dpath, entry.Name))
		if err != nil {
			return report, nil, err
		}
	}

DONE:
	return report, symlinkCallbacks, err
}

// DecompileImage will copy the contents inside vorteilImage to the outputPath on the local filesystem.
//	If skipNotTouched is set to true, only files that have been touched during runtime will be copied.
//	Returns a DecompileReport Object that provides information of the result of each file.
func DecompileImage(vorteilImage *vdecompiler.IO, outputPath string, skipNotTouched bool) (DecompileReport, error) {
	report := DecompileReport{
		ImageFiles:     make([]DecompiledFile, 0),
		SkipNotTouched: skipNotTouched,
	}

	fi, err := os.Stat(outputPath)
	if err != nil && !os.IsNotExist(err) {
		return report, err
	}
	var into bool
	if !os.IsNotExist(err) && fi.IsDir() {
		into = true
	}

	fpath := "/"
	dpath := outputPath
	if into {
		dpath = filepath.ToSlash(filepath.Join(outputPath, filepath.Base(fpath)))
	}

	symlinkCallbacks := make([]func() error, 0)

	ino, err := vorteilImage.ResolvePathToInodeNo(fpath)
	if err != nil {
		return report, err
	}
	report, symlinkCallbacks, err = decompileImageRecursive(vorteilImage, report, symlinkCallbacks, ino, filepath.ToSlash(filepath.Base(fpath)), dpath)
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
