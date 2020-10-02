package vio

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TODO: test FileTree.Unmap
// TODO: test FileTree.SubTree
// TODO: Check that overwrites clean up the things they replace
// TODO: test FileTree.MapSubTree
// TODO: FileTreeFromDirectory

func TestFileTreeArchive(t *testing.T) {

	var err error

	tree := NewFileTree()

	addToTree := func(id string) error {
		f := CustomFile(CustomFileArgs{
			Name:       filepath.Base(id),
			Size:       len(id),
			ReadCloser: ioutil.NopCloser(strings.NewReader(id)),
		})
		return tree.Map(id, f)
	}

	nodes := []string{"A", "C", "B/bravo", "a"}
	for _, id := range nodes {
		err = addToTree(id)
		if err != nil {
			t.Error(err)
			return
		}
	}

	buf := new(bytes.Buffer)
	err = tree.Archive(buf, func(path string, f File) error {
		return nil
	})
	if err != nil {
		t.Error(err)
		return
	}

	err = tree.Close()
	if err != nil {
		t.Error(err)
		return
	}

	tree, err = LoadArchive(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Error(err)
		return
	}

	var got []string
	err = tree.Walk(func(path string, f File) error {

		if f.IsDir() {
			return nil
		}

		data, err := ioutil.ReadAll(f)
		if err != nil {
			return err
		}

		path = strings.TrimPrefix(path, "./")

		if string(data) != path {
			return fmt.Errorf("bad file data: expected %s, but got %s", path, data)
		}

		got = append(got, path)
		return nil

	})
	if err != nil {
		t.Error(err)
		return
	}

	sort.Strings(nodes)
	g := fmt.Sprintf("%v", got)
	e := fmt.Sprintf("%v", nodes)
	if g != e {
		t.Errorf("FileTree.Archive -> FileTree.LoadArchive produced unexpected results: expected %s but got %s", e, g)
		return
	}

}

func TestFileTreeCloseOrder(t *testing.T) {

	var err error

	open := func() (io.Reader, error) {
		return Zeroes, nil
	}

	var closedIDs []string

	closefn := func(id string) func() error {
		return func() error {
			closedIDs = append(closedIDs, id)
			return nil
		}
	}

	tree := NewFileTree()

	addToTree := func(id string) error {
		f := CustomFile(CustomFileArgs{
			Name:       filepath.Base(id),
			ReadCloser: LazyReadCloser(open, closefn(id)),
		})
		return tree.Map(id, f)
	}

	nodes := []string{"A", "C", "B/bravo", "a"}
	for _, id := range nodes {
		err = addToTree(id)
		if err != nil {
			t.Error(err)
			return
		}
	}

	err = tree.Close()
	if err != nil {
		t.Error(err)
		return
	}

	sort.Strings(nodes)
	g := fmt.Sprintf("%v", closedIDs)
	e := fmt.Sprintf("%v", nodes)
	if g != e {
		t.Errorf("FileTree.Close produced unexpected results: expected %s but got %s", e, g)
		return
	}

}

func TestFileTreeCloseError(t *testing.T) {

	var err error

	errExpect := errors.New("test")

	tree := NewFileTree()

	addToTree := func(id string) error {
		f := CustomFile(CustomFileArgs{
			Name: filepath.Base(id),
			ReadCloser: LazyReadCloser(func() (io.Reader, error) {
				return Zeroes, nil
			}, func() error {
				return errExpect
			}),
		})
		return tree.Map(id, f)
	}

	nodes := []string{"A", "C", "B/bravo", "a"}
	for _, id := range nodes {
		err = addToTree(id)
		if err != nil {
			t.Error(err)
			return
		}
	}

	err = tree.Close()
	if err != errExpect {
		t.Errorf("FileTree.Close failed to return correct error: expected %v but got %v", errExpect, err)
		return
	}

}

func TestFileTreeNodeCount(t *testing.T) {

	var err error

	tree := NewFileTree()
	defer tree.Close()

	addToTree := func(id string) error {
		f := CustomFile(CustomFileArgs{
			Name:       filepath.Base(id),
			ReadCloser: ioutil.NopCloser(Zeroes),
		})
		return tree.Map(id, f)
	}

	nodes := []string{"A", "C", "B/bravo", "a"}
	for _, id := range nodes {
		err = addToTree(id)
		if err != nil {
			t.Error(err)
			return
		}
	}

	e := 6
	g := tree.NodeCount()
	if g != e {
		t.Errorf("FileTree.NodeCount failed to return correct number: expected %v but got %v", e, g)
	}

}

func TestFileTreeMapRoot(t *testing.T) {

	var err error
	var tree FileTree

	addToTree := func(name, path string) error {
		f := CustomFile(CustomFileArgs{
			Name:       name,
			ReadCloser: ioutil.NopCloser(Zeroes),
		})
		return tree.Map(path, f)
	}

	rootTest := func(name, path string) error {
		tree = NewFileTree()
		err = addToTree(name, path)
		if err == nil || err.Error() != "cannot map over the root node" {
			if err == nil {
				err = errors.New("didn't get an error mapping over the root directory")
			}
			t.Error(err)
			return err
		}
		return nil
	}

	err = rootTest("a", "")
	if err != nil {
		return
	}

	err = rootTest("b", "/")
	if err != nil {
		return
	}

	err = rootTest("c", ".")
	if err != nil {
		return
	}

	err = rootTest("d", "./")
	if err != nil {
		return
	}

	err = rootTest("e", "./.")
	if err != nil {
		return
	}

	err = rootTest("f", "..")
	if err != nil {
		return
	}

	err = rootTest("g", ".././..")
	if err != nil {
		return
	}

}

func TestFileTreeMap(t *testing.T) {

	var err error
	var tree FileTree
	var paths, names []string

	addToTree := func(name, path string) error {
		f := CustomFile(CustomFileArgs{
			Name:       name,
			ReadCloser: ioutil.NopCloser(Zeroes),
		})
		return tree.Map(path, f)
	}

	mapTest := func(name, path string, expect ...string) error {
		tree = NewFileTree()
		paths = []string{}
		names = []string{}
		err = addToTree(name, path)
		if err != nil {
			t.Error(err)
			return err
		}
		err = tree.Walk(func(path string, f File) error {
			paths = append(paths, path)
			names = append(names, f.Name())
			return nil
		})
		if err != nil {
			t.Error(err)
			return err
		}
		tree.Close()
		if len(paths) != len(expect) {
			err = fmt.Errorf("FileTree.Map to a path created the wrong number of nodes: expected %v, got %v (path: %s)", len(expect), len(paths), path)
			t.Error(err)
			return err
		}
		e := fmt.Sprintf("%v", expect)
		g := fmt.Sprintf("%v", paths)
		if e != g {
			err = fmt.Errorf("FileTree.Map to a path returned an unexpected path: expected %v, got %v", e, g)
			t.Error(err)
			return err
		}
		return nil
	}

	err = mapTest("a", "a", ".", "./a")
	if err != nil {
		return
	}

	err = mapTest("a", "./a", ".", "./a")
	if err != nil {
		return
	}

	err = mapTest("a", "/a", ".", "./a")
	if err != nil {
		return
	}

	err = mapTest("a", "a/b/c", ".", "./a", "./a/b", "./a/b/c")
	if err != nil {
		return
	}

	err = mapTest("a", "a/../a/b/c", ".", "./a", "./a/b", "./a/b/c")
	if err != nil {
		return
	}

	err = mapTest("a", "../../a/../a/b/c", ".", "./a", "./a/b", "./a/b/c")
	if err != nil {
		return
	}

}
