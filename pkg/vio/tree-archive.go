package vio

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"archive/tar"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"strings"
)

const (
	magicNumber = 0x45455254454C4946 // FILETREE
)

type archiveHeader struct {
	Magic   uint64
	MetaLen int64
	Pad     [496]byte
}

var errCorruptArchive = errors.New("corrupt archive data")

func readArchiveHeader(r io.Reader) (*archiveHeader, error) {

	hdr := new(archiveHeader)
	err := binary.Read(r, binary.LittleEndian, hdr)
	if err != nil {
		return nil, fmt.Errorf("failed to read vtree archive header: %w", err)
	}

	if hdr.Magic != magicNumber {
		err = errors.New("not a vtree archive")
		return nil, err
	}

	return hdr, nil

}

func readArchiveManifest(r io.Reader, hdr *archiveHeader) ([]byte, error) {

	l := hdr.MetaLen
	dif := 512 - l%512

	buf := new(bytes.Buffer)
	_, err := io.CopyN(buf, r, int64(l))
	if err != nil {
		return nil, err
	}

	_, err = io.CopyN(ioutil.Discard, r, int64(dif))
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil

}

func readArchiveMetadata(r io.Reader) (map[string]interface{}, error) {

	hdr, err := readArchiveHeader(r)
	if err != nil {
		return nil, err
	}

	data, err := readArchiveManifest(r, hdr)
	if err != nil {
		return nil, err
	}

	m := make(map[string]interface{})
	err = json.Unmarshal(data, &m)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal vtree manifest: %w -- archive corrupt or version incompatible", err)
	}

	return m, nil

}

type archiveLoader struct {
	tr *tar.Reader
}

func (a *archiveLoader) loadChildren(n *TreeNode, x map[string]interface{}) error {

	// load children
	v, ok := x["children"]
	if !ok {
		return errCorruptArchive
	}

	if v != nil {
		slice, ok := v.([]interface{})
		if !ok {
			err := errCorruptArchive
			return err
		}

		for _, elem := range slice {
			data, err := json.Marshal(elem)
			if err != nil {
				panic(err)
			}

			y := make(map[string]interface{})
			err = json.Unmarshal(data, &y)
			if err != nil {
				panic(err)
			}

			z, err := a.reconstructArchiveNode(n, y)
			if err != nil {
				return err
			}

			n.Children = append(n.Children, z)
		}
	}

	return nil

}

func (a *archiveLoader) readFileInfo(parent *TreeNode, x map[string]interface{}) (*fi, error) {

	v, ok := x["fi"]
	if !ok {
		return nil, errCorruptArchive
	}

	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	fi := new(fi)
	err = json.Unmarshal(data, fi)
	if err != nil {
		return nil, err
	}

	if fi.Size < 0 {
		return nil, fmt.Errorf("object '%s' had non zero size", filepath.Join(parent.path(), fi.Name))
	}

	return fi, nil

}

func (a *archiveLoader) openFn(parentPath string, fi *fi) func() (io.Reader, error) {

	return func() (io.Reader, error) {

		path := parentPath + "/" + fi.Name

		for {
			h, err := a.tr.Next()
			if err != nil {
				return nil, err
			}

			if h.Name == path {
				if h.Linkname != "" {
					return strings.NewReader(h.Linkname), nil
				}
				return a.tr, nil
			}
		}
	}

}

func (a *archiveLoader) nilClose() error {
	return nil
}

func (a *archiveLoader) reconstructArchiveNode(parent *TreeNode, x map[string]interface{}) (*TreeNode, error) {

	fi, err := a.readFileInfo(parent, x)
	if err != nil {
		return nil, err
	}

	parentPath := "."
	if parent != nil {
		parentPath = parent.path()
	}

	var rc io.ReadCloser
	if fi.IsSymlink && fi.Symlink != "" {
		rc = ioutil.NopCloser(strings.NewReader(fi.Symlink))
	} else {
		rc = LazyReadCloser(a.openFn(parentPath, fi), a.nilClose)
	}

	f := CustomFile(CustomFileArgs{
		Name:               fi.Name,
		Size:               fi.Size,
		ModTime:            fi.ModTime,
		IsDir:              fi.IsDir,
		IsSymlink:          fi.IsSymlink,
		IsSymlinkNotCached: fi.Symlink == "",
		Symlink:            fi.Symlink,
		ReadCloser:         rc,
	})

	n := &TreeNode{
		File:   f,
		Parent: parent,
	}

	err = a.loadChildren(n, x)
	if err != nil {
		return nil, err
	}

	return n, nil

}

func archiveCloser(r io.Reader) func() error {

	return func() error {

		if closer, ok := r.(io.Closer); ok {

			defer closer.Close()

			_, err := io.Copy(ioutil.Discard, r)
			if err != nil {
				return err
			}

			err = closer.Close()
			if err != nil {
				return err
			}

		}

		return nil

	}

}

// LoadArchive reads a stream of data created by the
// FileTree.Archive function and loads it as a new FileTree.
// This can be used for storing FileTrees or transferring
// them over the network.
//
// LoadArchive loads the FileTree using lazy loading, and
// does not need to cache the entire contents of r within
// memory.
func LoadArchive(r io.Reader) (FileTree, error) {

	m, err := readArchiveMetadata(r)
	if err != nil {
		return nil, err
	}

	tr := tar.NewReader(r)

	a := &archiveLoader{
		tr: tr,
	}

	root, err := a.reconstructArchiveNode(nil, m)
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct vtree node: %w -- archive corrupt or version incompatible", err)
	}

	t := &tree{
		root: root,
	}

	t.closeFunc = archiveCloser(r)

	return t, nil

}

type archiver struct {
	tarw *tar.Writer
	fn   ArchiveFunc
}

func (a *archiver) writeSymlink(path string, f File) error {

	var link string
	var data []byte
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}
	link = string(data)

	hdr, err := tar.FileInfoHeader(Info(f), link)
	if err != nil {
		return err
	}

	hdr.Name = path

	err = a.tarw.WriteHeader(hdr)
	if err != nil {
		return err
	}

	return f.Close()

}

func (a *archiver) writeDir(path string, f File) error {

	hdr, err := tar.FileInfoHeader(Info(f), "")
	if err != nil {
		return err
	}

	hdr.Name = path

	err = a.tarw.WriteHeader(hdr)
	if err != nil {
		return err
	}

	return f.Close()

}

func (a *archiver) writeFile(path string, f File) error {

	hdr, err := tar.FileInfoHeader(Info(f), "")
	if err != nil {
		return err
	}

	hdr.Name = path

	err = a.tarw.WriteHeader(hdr)
	if err != nil {
		return err
	}

	_, err = io.Copy(a.tarw, f)
	if err != nil {
		return err
	}

	return f.Close()

}

func (a *archiver) walker(path string, f File) error {

	var err error

	if a.fn != nil {
		err = a.fn(path, f)
		if err != nil {
			return err
		}
	}

	if f.IsSymlink() {
		return a.writeSymlink(path, f)
	}

	if f.IsDir() {
		return a.writeDir(path, f)
	}

	return a.writeFile(path, f)

}

func (t *tree) writeArchiveHeader(w io.Writer) error {

	data, err := t.root.MarshalJSON()
	if err != nil {
		panic(err)
	}

	l := len(data)

	hdr := archiveHeader{
		Magic:   magicNumber,
		MetaLen: int64(l),
	}

	err = binary.Write(w, binary.LittleEndian, hdr)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, bytes.NewReader(data))
	if err != nil {
		return err
	}

	dif := 512 - l%512
	_, err = io.Copy(w, bytes.NewReader(bytes.Repeat([]byte{0}, dif)))
	if err != nil {
		return err
	}

	return nil

}

func (t *tree) Archive(w io.Writer, fn ArchiveFunc) error {

	var err error

	err = t.writeArchiveHeader(w)
	if err != nil {
		return err
	}

	tarw := tar.NewWriter(w)
	defer tarw.Close()

	a := &archiver{
		tarw: tarw,
		fn:   fn,
	}
	err = t.Walk(a.walker)
	if err != nil {
		return err
	}

	err = tarw.Close()
	if err != nil {
		return err
	}

	return nil
}
