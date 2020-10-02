package vio

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// File represents a file from the filesystem.
type File interface {

	// Name returns the base name of the file, not a
	// full path (see filepath.Base).
	Name() string

	// Size returns the size of the file in bytes. If
	// the file represents a directory the size returned
	// should be zero.
	Size() int

	// ModTime returns the time the file was most
	// recently modified.
	ModTime() time.Time

	// Read implements io.Reader to retrieve file
	// contents.
	Read(p []byte) (n int, err error)

	// Close implements io.Closer.
	Close() error

	// IsDir returns true if the File represents a
	// directory.
	IsDir() bool

	// IsSymlink returns true if the File represents a symlink.
	IsSymlink() bool

	// SymlinkIsCached returns true if the symlink can be read without messing with a filetree order.
	SymlinkIsCached() bool

	// Symlink returns a non-empty string if the symlink is cached, or an empty string otherwise.
	Symlink() string
}

// Open mimics the os.Open function but returns an
// implementation of File.
func Open(path string) (File, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	if fi.Mode()&os.ModeSymlink == os.ModeSymlink {

		lpath, err := os.Readlink(path)
		if err != nil {
			return nil, err
		}
		lpath = filepath.ToSlash(lpath)

		rdr := strings.NewReader(lpath)
		rc := ioutil.NopCloser(rdr)

		return CustomFile(CustomFileArgs{
			Name:       fi.Name(),
			Size:       len(lpath),
			ModTime:    fi.ModTime(),
			IsDir:      fi.IsDir(),
			IsSymlink:  true,
			ReadCloser: rc,
		}), nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	return CustomFile(CustomFileArgs{
		Name:       fi.Name(),
		Size:       int(fi.Size()),
		ModTime:    fi.ModTime(),
		IsDir:      fi.IsDir(),
		IsSymlink:  false,
		ReadCloser: f,
	}), nil
}

// CustomFileArgs takes all elements that need to be provided
// to the CustomFile function.
type CustomFileArgs struct {
	Name               string
	Size               int
	ModTime            time.Time
	IsDir              bool
	IsSymlink          bool
	IsSymlinkNotCached bool
	Symlink            string
	ReadCloser         io.ReadCloser
}

// CustomFile makes it possible to construct a custom file
// that implements the File interface without necessarily
// being backed by an actual file on the filesystem.
func CustomFile(args CustomFileArgs) File {
	return &customFile{
		name:            args.Name,
		size:            args.Size,
		modTime:         args.ModTime,
		isDir:           args.IsDir,
		isSymlink:       args.IsSymlink,
		isSymlinkCached: !args.IsSymlinkNotCached,
		symlink:         args.Symlink,
		rc:              args.ReadCloser,
	}
}

type customFile struct {
	name            string
	size            int
	modTime         time.Time
	isDir           bool
	isSymlink       bool
	isSymlinkCached bool
	symlink         string
	rc              io.ReadCloser
}

func (f *customFile) Name() string {
	return f.name
}

func (f *customFile) Size() int {
	return f.size
}

func (f *customFile) ModTime() time.Time {
	return f.modTime
}

func (f *customFile) IsDir() bool {
	return f.isDir
}

func (f *customFile) IsSymlink() bool {
	return f.isSymlink
}

func (f *customFile) SymlinkIsCached() bool {
	return f.isSymlinkCached
}

func (f *customFile) Symlink() string {
	return f.symlink
}

func (f *customFile) Read(p []byte) (n int, err error) {
	return f.rc.Read(p)
}

func (f *customFile) Close() error {
	if f.rc != nil {
		return f.rc.Close()
	}
	return nil
}

// finfo exists to implement os.FileInfo for this package's
// FileInfo function.
type finfo struct {
	name    string
	size    int64
	modtime time.Time
	mode    os.FileMode
}

func (fi *finfo) Name() string {
	return fi.name
}

func (fi *finfo) Size() int64 {
	return fi.size
}

func (fi *finfo) ModTime() time.Time {
	return fi.modtime
}

func (fi *finfo) Mode() os.FileMode {
	return fi.mode
}

func (fi *finfo) IsDir() bool {
	return fi.mode.IsDir()
}

func (fi *finfo) Sys() interface{} {
	return nil
}

// Info produces an implementation of os.FileInfo from a an
// implementation of File.
func Info(f File) os.FileInfo {
	mode := os.ModePerm
	if f.IsDir() {
		mode |= os.ModeDir
	}
	if f.IsSymlink() {
		mode |= os.ModeSymlink
	}
	return &finfo{
		name:    f.Name(),
		size:    int64(f.Size()),
		modtime: f.ModTime(),
		mode:    mode,
	}
}

// LazyReadCloser is an implementation of io.ReadCloser
// that defers its own initialization until the first
// attempted read.
func LazyReadCloser(openFunc func() (io.Reader, error),
	closeFunc func() error) io.ReadCloser {
	return &lazyReadCloser{
		openFunc:  openFunc,
		closeFunc: closeFunc,
	}
}

type lazyReadCloser struct {
	opened    bool
	closed    bool
	r         io.Reader
	openFunc  func() (io.Reader, error)
	closeFunc func() error
}

func (rc *lazyReadCloser) Read(p []byte) (n int, err error) {
	if rc.closed {
		err = errors.New("lazy readcloser is closed")
		return
	}

	if rc.r == nil {
		rc.r, err = rc.openFunc()
		if err != nil {
			return
		}
		rc.opened = true
	}

	return rc.r.Read(p)
}

func (rc *lazyReadCloser) Close() error {
	if rc.closed {
		return errors.New("lazy readcloser already closed")
	}
	rc.closed = true
	return rc.closeFunc()
}

// LazyOpen is an alternative implementation of Open that
// defers actually opening the file until the first
// attempted read.
func LazyOpen(path string) (File, error) {

	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	var f *os.File
	var lpath string
	var lrdr io.Reader
	if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
		lpath, err = os.Readlink(path)
		if err != nil {
			return nil, err
		}
		lpath = filepath.ToSlash(lpath)
		lrdr = strings.NewReader(lpath)
	}

	openFunc := func() (io.Reader, error) {

		var err error

		if lrdr != nil {
			return lrdr, nil
		}

		f, err = os.Open(path)
		if err != nil {
			return nil, err
		}
		return f, nil
	}

	closeFunc := func() error {

		if f != nil {
			return f.Close()
		}
		return nil
	}

	var fsize int
	fsize = int(fi.Size())
	var islink bool
	islink = fi.Mode()&os.ModeSymlink == os.ModeSymlink
	if islink && fsize == 0 {
		p, err := os.Readlink(path)
		p = filepath.ToSlash(p)
		if err != nil {
			return nil, err
		}
		fsize = len(p)
	}

	return CustomFile(CustomFileArgs{
		Name:               fi.Name(),
		Size:               fsize,
		ModTime:            fi.ModTime(),
		IsDir:              fi.IsDir(),
		IsSymlink:          islink,
		IsSymlinkNotCached: false,
		Symlink:            lpath,
		ReadCloser:         LazyReadCloser(openFunc, closeFunc),
	}), nil
}
