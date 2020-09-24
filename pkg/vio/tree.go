package vio

import (
	"archive/tar"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNodeNotFound is returned when attempting to look up a
// node within a FileTree that does not exist.
var ErrNodeNotFound = errors.New("node not found")

// ArchiveFunc is the type of function called for each file
// or directory immediately before it is streamed into an
// archive with the FileTree.Archive function.
type ArchiveFunc func(path string, f File) error

// WalkFunc is the type of function called for each file or
// directory visited by FileTree.Walk. The root node will
// have path ".", and all other nodes will be built from
// that (e.g. "./a").
type WalkFunc func(path string, f File) error

// WalkNodeFunc is the type of function called for each node
// visited by FileTree.WalkNode.
type WalkNodeFunc func(path string, n *TreeNode) error

// ErrSkip can be passed as the result from a WalkFunc to
// tell FileTree.Walk to skip the remainder of the directory.
var ErrSkip = errors.New("skip")

// FileTree represents a tree of files and directories it is
// used to organize, modify, and transfer the data that will
// become the filesystem for an application.
type FileTree interface {
	Close() error

	// Archive encodes the data within the FileTree into
	// a stream that it writes to w. This stream can be
	// decoded by calling LoadArchive.
	//
	// The fn argument is optional, and can be used to
	// track progress or perform logging during the
	// archiving process.
	Archive(w io.Writer, fn ArchiveFunc) error

	// Map adds f to the FileTree at path. It automatically
	// creates parent directories (recursively) if necessary,
	// and it automatically replaces any existing nodes
	// within the tree if there are collisions, calling
	// the Close method recursively on all replaced
	// nodes.
	//
	// Mapping a directory over an existing directory
	// node does not delete all existing nodes under the
	// directory, but instead merges over the top of
	// them, only replacing nodes with the same name.
	Map(path string, f File) error

	// MapSubTree adds t to the FileTree as a sub-tree
	// at path. It automatically creates parent directories
	// (recursively) if necessary, and it automatically
	// replaces any existing nodes within the tree if
	// there are collisions, calling the Close
	// method recursively on all replaced nodes.
	MapSubTree(path string, t FileTree) error

	// SubTree returns a new FileTree object where the
	// root node is the directory node at path.
	SubTree(path string) (FileTree, error)

	// Unmap removes a node from the FileTree, calling
	// the Close method recursively on all removed
	// nodes.
	Unmap(path string) error

	// Walk traverses the FileTree recursively in a
	// pre-order tree traversal.
	Walk(fn WalkFunc) error

	// WalkNode traverses the FileTree recursively and
	// passes in a complete tree node so we can learn
	// more about it's place in the tree.
	WalkNode(fn WalkNodeFunc) error

	NodeCount() int
}

var errCorruptArchive = errors.New("corrupt archive data")

// LoadArchive reads a stream of data created by the
// FileTree.Archive function and loads it as a new FileTree.
// This can be used for storing FileTrees or transferring
// them over the network.
//
// LoadArchive loads the FileTree using lazy loading, and
// does not need to cache the entire contents of r within
// memory.
func LoadArchive(r io.Reader) (FileTree, error) {

	hdr := new(archiveHeader)
	err := binary.Read(r, binary.LittleEndian, hdr)
	if err != nil {
		return nil, fmt.Errorf("failed to read vtree archive header: %w", err)
	}

	if hdr.Magic != magicNumber {
		err = errors.New("not a vtree archive")
		return nil, err
	}

	l := hdr.MetaLen
	dif := 512 - l%512

	buf := new(bytes.Buffer)
	_, err = io.CopyN(buf, r, int64(l))
	if err != nil {
		return nil, err
	}

	_, err = io.CopyN(ioutil.Discard, r, int64(dif))
	if err != nil {
		return nil, err
	}

	data := buf.Bytes()
	tr := tar.NewReader(r)
	m := make(map[string]interface{})
	err = json.Unmarshal(data, &m)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal vtree manifest: %w -- archive corrupt or version incompatible", err)
	}

	var reconstructNode func(parent *TreeNode, x map[string]interface{}) (*TreeNode, error)
	reconstructNode = func(parent *TreeNode, x map[string]interface{}) (*TreeNode, error) {
		v, ok := x["fi"]
		if !ok {
			return nil, errCorruptArchive
		}

		data, err = json.Marshal(v)
		if err != nil {
			return nil, err
		}

		fi := new(fi)
		err = json.Unmarshal(data, fi)
		if err != nil {
			return nil, err
		}

		if fi.Size < 0 {
			return nil, fmt.Errorf("object '%s' had non zero size", filepath.Join(parent.path(), fi.Name))
		}

		openFn := func(parentPath string) func() (io.Reader, error) {
			return func() (io.Reader, error) {
				path := parentPath + "/" + fi.Name

				for {
					h, err := tr.Next()
					if err != nil {
						return nil, err
					}

					if h.Name == path {
						if h.Linkname != "" {
							return strings.NewReader(h.Linkname), nil
						}
						return tr, nil
					}
				}
			}
		}

		closeFn := func() error {
			return nil
		}

		parentPath := "."
		if parent != nil {
			parentPath = parent.path()
		}

		var rc io.ReadCloser
		if fi.IsSymlink && fi.Symlink != "" {
			rc = ioutil.NopCloser(strings.NewReader(fi.Symlink))
		} else {
			rc = LazyReadCloser(openFn(parentPath), closeFn)
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

		// load children
		v, ok = x["children"]
		if !ok {
			return nil, errCorruptArchive
		}

		if v != nil {
			slice, ok := v.([]interface{})
			if !ok {
				err = errCorruptArchive
				return nil, err
			}

			for _, elem := range slice {
				data, err = json.Marshal(elem)
				if err != nil {
					return nil, err
				}

				y := make(map[string]interface{})
				err = json.Unmarshal(data, &y)
				if err != nil {
					return nil, err
				}

				z, err := reconstructNode(n, y)
				if err != nil {
					return nil, err
				}

				n.Children = append(n.Children, z)
			}
		}

		return n, nil

	}

	root, err := reconstructNode(nil, m)
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct vtree node: %w -- archive corrupt or version incompatible", err)
	}

	t := &tree{
		root: root,
	}

	if closer, ok := r.(io.Closer); ok {
		t.closeFunc = func() error {
			defer closer.Close()
			_, err := io.Copy(ioutil.Discard, r)
			if err != nil {
				return err
			}

			err = closer.Close()
			if err != nil {
				return err
			}

			return nil
		}
	}

	return t, nil
}

type tree struct {
	root       *TreeNode
	lock       sync.Mutex
	closed     bool
	closeFunc  func() error
	walked     bool
	walkedLock sync.Mutex
	nodeCount  int
}

// TreeNode is the structure that all nodes in a FileTree are built on.
type TreeNode struct {
	File               File
	Parent             *TreeNode
	Children           []*TreeNode
	NodeSequenceNumber int64
	Links              int
}

type fi struct {
	Name      string
	Size      int
	IsDir     bool
	IsSymlink bool
	Symlink   string
	ModTime   time.Time
}

// MarshalJSON implements json.Marshaler.
func (n *TreeNode) MarshalJSON() ([]byte, error) {

	if n.File.Size() < 0 {
		return nil, fmt.Errorf("failed to marshal JSON due to broken tree node: '%s' has size '%d'", n.path(), n.File.Size())
	}

	m := make(map[string]interface{})
	info := &fi{
		Name:      n.File.Name(),
		Size:      n.File.Size(),
		IsDir:     n.File.IsDir(),
		IsSymlink: n.File.IsSymlink(),
		ModTime:   n.File.ModTime(),
	}

	if n.File.IsSymlink() && n.File.SymlinkIsCached() {
		info.Symlink = n.File.Symlink()
	}

	m["fi"] = info

	m["children"] = n.Children
	return json.Marshal(m)
}

func (n *TreeNode) path() string {

	if n.Parent == nil {
		return n.File.Name()
	}

	if n.Parent == n {
		return n.File.Name()
	}

	p := filepath.Join(n.Parent.path(), n.File.Name())
	p = "./" + p
	p = filepath.ToSlash(p)

	return p

}

func (n *TreeNode) mapIn(path string, f File) error {

	var err error
	var next, rest string
	strs := strings.SplitN(path, "/", 2)
	next = strs[0]
	if len(strs) == 2 {
		rest = strs[1]
	}

	newNode := &TreeNode{
		Parent:   n,
		Children: []*TreeNode{},
	}

	if rest == "" {
		newNode.File = f
	} else {
		newNode.File = CustomFile(CustomFileArgs{
			Name:       next,
			IsDir:      true,
			ModTime:    f.ModTime(), // really?
			Size:       0,
			ReadCloser: ioutil.NopCloser(strings.NewReader("")),
		})
	}

	l := len(n.Children)

	k := sort.Search(l, func(i int) bool {
		return next <= n.Children[i].File.Name()
	})

	if k == l {
		// append new node
		if rest != "" {
			err = newNode.mapIn(rest, f)
			if err != nil {
				return err
			}
		}
		n.Children = append(n.Children, newNode)
		return nil
	}

	child := n.Children[k]
	if next == child.File.Name() {
		if child.File.IsDir() && newNode.File.IsDir() {
			// merge
			if rest != "" {
				err = child.mapIn(rest, f)
				if err != nil {
					return err
				}
			}
		} else {
			// replace
			err = child.walk(func(path string, f File) error {
				return f.Close()
			})
			if err != nil {
				return err
			}

			n.Children[k] = newNode
		}

		return nil
	}

	// insert
	if rest != "" {
		err = newNode.mapIn(rest, f)
		if err != nil {
			return err
		}
	}
	n.Children = append(n.Children[:k],
		append([]*TreeNode{newNode}, n.Children[k:]...)...)

	return nil

}

func (n *TreeNode) mapInSubTree(path string, sub FileTree) error {

	var err error
	var next, rest string
	strs := strings.SplitN(path, "/", 2)
	next = strs[0]
	if len(strs) == 2 {
		next = strs[1]
	}

	var newNode *TreeNode
	if rest == "" {
		newNode = sub.(*tree).root
	} else {
		data := ioutil.NopCloser(strings.NewReader(""))
		var mt time.Time
		mt, err = time.ParseInLocation(time.RFC3339, "1970-01-01T00:00:00Z", time.UTC)
		if err != nil {
			return err
		}

		newNode = &TreeNode{
			File: CustomFile(CustomFileArgs{
				Name:  next,
				IsDir: true,
				// ModTime:    time.Unix(0, 0),
				ModTime:    mt,
				Size:       0,
				ReadCloser: data,
			}),
			Parent:   n,
			Children: []*TreeNode{},
		}
	}

	l := len(n.Children)

	k := sort.Search(l, func(i int) bool {
		return next <= n.Children[i].File.Name()
	})

	if k == l {
		// append new node
		if rest != "" {
			err = newNode.mapInSubTree(rest, sub)
			if err != nil {
				return err
			}
		}
		n.Children = append(n.Children, newNode)
		return nil
	}

	child := n.Children[k]
	if next == child.File.Name() {
		if child.File.IsDir() && newNode.File.IsDir() {
			for _, nc := range newNode.Children {
				i := sort.Search(len(child.Children), func(i int) bool {
					return child.Children[i].File.Name() <= nc.File.Name()
				})
				if i >= len(child.Children) {
					child.Children = append(child.Children, nc)
				} else {
					if child.Children[i].File.Name() == nc.File.Name() {
						panic("unexpected tree merge error")
					} else {
						child.Children = append(child.Children[:i], append([]*TreeNode{nc}, child.Children[i:]...)...)
					}
				}
			}
			return nil
			// merge
			// don't need to to anything here
		}
		// replace
		err = child.walk(func(path string, f File) error {
			return f.Close()
		})
		if err != nil {
			return err
		}

		n.Children[k] = newNode
		return nil
	}

	// insert
	if rest != "" {
		err = newNode.mapInSubTree(rest, sub)
		if err != nil {
			return err
		}
	}
	n.Children = append(n.Children[:k],
		append([]*TreeNode{newNode}, n.Children[k:]...)...)

	return nil

}

func (n *TreeNode) unmap(path string) error {

	var err error
	var next, rest string
	strs := strings.SplitN(path, "/", 2)
	next = strs[0]
	if len(strs) == 2 {
		next = strs[1]
	}

	l := len(n.Children)

	k := sort.Search(l, func(i int) bool {
		return next < n.Children[i].File.Name()
	})

	if k == l {
		return ErrNodeNotFound
	}

	child := n.Children[k]
	if next == child.File.Name() {
		if rest != "" {
			return child.unmap(rest)
		}
		err = child.walk(func(path string, f File) error {
			return f.Close()
		})
		if err != nil {
			return err
		}

		n.Children = append(n.Children[:k], n.Children[k+1:]...)
		return nil
	}

	return ErrNodeNotFound

}

func (n *TreeNode) walk(fn WalkFunc) error {

	var err error
	var isDir = n.File.IsDir()

	err = fn(n.path(), n.File)
	if err == nil && isDir {
		for _, child := range n.Children {
			err = child.walk(fn)
			if err != nil {
				break
			}
		}
	}

	if err == ErrSkip && isDir {
		return nil
	}

	if err != nil {
		return err
	}

	return nil

}

func (n *TreeNode) walkNode(fn WalkNodeFunc) error {

	var err error
	var isDir = n.File.IsDir()

	err = fn(n.path(), n)
	if err == nil && isDir {
		for _, child := range n.Children {
			err = child.walkNode(fn)
			if err != nil {
				break
			}
		}
	}

	if err == ErrSkip && isDir {
		return nil
	}

	if err != nil {
		return err
	}

	return nil

}

// NewFileTree returns a new filetree with an empty root directory.
func NewFileTree() FileTree {
	data := ioutil.NopCloser(strings.NewReader(""))

	mt, err := time.ParseInLocation(time.RFC3339, "1970-01-01T00:00:00Z", time.UTC)
	if err != nil {
		panic(err)
	}

	return &tree{
		root: &TreeNode{
			File: CustomFile(CustomFileArgs{
				Name:  ".",
				Size:  0,
				IsDir: true,
				// ModTime:    time.Unix(0, 0),
				ModTime:    mt,
				ReadCloser: data,
			}),
			Parent:   nil,
			Children: []*TreeNode{},
		},
	}
}

// FileTreeFromDirectory ..
func FileTreeFromDirectory(dir string) (FileTree, error) {
	tree := NewFileTree()
	err := filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {

		path = filepath.ToSlash(path)
		abs := path
		path = strings.TrimPrefix(path, dir)
		path = strings.TrimPrefix(path, "/")
		if path == "" {
			return nil
		}

		f, err := LazyOpen(abs)
		if err != nil {
			return err
		}

		err = tree.Map(path, f)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return tree, nil
}

const (
	magicNumber = 0x45455254454C4946 // FILETREE
)

type archiveHeader struct {
	Magic   uint64
	MetaLen int64
	Pad     [496]byte
}

func (t *tree) Close() error {

	t.lock.Lock()
	defer t.lock.Unlock()
	if t.closed {
		return errors.New("already closed")
	}
	t.closed = true
	if t.closeFunc != nil {
		defer t.closeFunc()
	}
	err := t.Walk(func(path string, f File) error {
		return f.Close()
	})
	if err != nil {
		return err
	}
	if t.closeFunc != nil {
		return t.closeFunc()
	}

	return nil
}

func (t *tree) computeMetadata() {
	t.walkedLock.Lock()
	if !t.walked {
		idx := int64(0)
		err := t.root.walkNode(func(path string, n *TreeNode) error {
			n.NodeSequenceNumber = idx
			t.nodeCount++
			n.Links++ // assume one parent
			if n.File.IsDir() {
				n.Links++ // link to self
				for _, child := range n.Children {
					if child.File.IsDir() {
						n.Links++ // child link back
					}
				}
			}
			idx++
			return nil
		})
		if err != nil {
			t.walkedLock.Unlock()
			panic(err)
		}
		t.walked = true
	}
	t.walkedLock.Unlock()
}

func (t *tree) NodeCount() int {
	t.computeMetadata()
	return t.nodeCount
}

func (t *tree) Archive(w io.Writer, fn ArchiveFunc) error {

	var err error

	data, err := t.root.MarshalJSON()
	if err != nil {
		return err
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

	tarw := tar.NewWriter(w)
	defer tarw.Close()

	err = t.Walk(func(path string, f File) error {

		var err error

		if fn != nil {
			err = fn(path, f)
			if err != nil {
				return err
			}
		}

		var link string
		if f.IsSymlink() {
			var data []byte
			data, err = ioutil.ReadAll(f)
			if err != nil {
				return err
			}
			link = string(data)
		}

		hdr, err := tar.FileInfoHeader(Info(f), link)
		if err != nil {
			return err
		}

		hdr.Name = path

		err = tarw.WriteHeader(hdr)
		if err != nil {
			return err
		}

		if !f.IsSymlink() && !f.IsDir() {

			var n int64
			n, err = io.Copy(tarw, f)
			if err != nil {
				return err
			}

			if n != hdr.Size {
				return err
			}

			err = f.Close()
			if err != nil {
				return err
			}
		}

		return nil

	})
	if err != nil {
		return err
	}

	err = tarw.Close()
	if err != nil {
		return err
	}

	return nil
}

func (t *tree) Map(path string, f File) error {

	if f.Size() < 0 {
		return errors.New("cannot map object with negative size")
	}

	t.lock.Lock()
	defer t.lock.Unlock()
	if t.closed {
		return errors.New("cannot map onto closed tree")
	}

	path = filepath.ToSlash(path)
	path = filepath.Clean(path)
	path = filepath.ToSlash(path)
	path = filepath.Join("/", path)
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return errors.New("cannot map over the root node")
	}

	f = CustomFile(CustomFileArgs{
		Name:               filepath.Base(path),
		Size:               f.Size(),
		IsDir:              f.IsDir(),
		IsSymlink:          f.IsSymlink(),
		IsSymlinkNotCached: !f.SymlinkIsCached(),
		Symlink:            f.Symlink(),
		ModTime:            f.ModTime(),
		ReadCloser:         f,
	})

	return t.root.mapIn(path, f)

}

func (t *tree) MapSubTree(path string, sub FileTree) error {

	t.lock.Lock()
	defer t.lock.Unlock()
	if t.closed {
		return errors.New("cannot map onto closed tree")
	}

	path = filepath.ToSlash(path)
	path = filepath.Clean(path)
	path = filepath.ToSlash(path)

	st := sub.(*tree)
	f := st.root.File
	st.root.File = CustomFile(CustomFileArgs{
		Name:               filepath.Base(path),
		Size:               f.Size(),
		IsDir:              f.IsDir(),
		IsSymlink:          f.IsSymlink(),
		IsSymlinkNotCached: !f.SymlinkIsCached(),
		Symlink:            f.Symlink(),
		ModTime:            f.ModTime(),
		ReadCloser:         f,
	})

	err := t.root.mapInSubTree(path, sub)
	if err != nil {
		return err
	}

	tmp := t.closeFunc
	t.closeFunc = func() error {
		if tmp != nil {
			e := tmp()
			if e != nil {
				return e
			}
		}

		return sub.Close()
	}

	return nil

}

func (t *tree) WalkNode(fn WalkNodeFunc) error {
	t.computeMetadata()
	return t.root.walkNode(fn)
}

func (t *tree) Walk(fn WalkFunc) error {
	return t.root.walk(fn)
}

func (t *tree) SubTree(path string) (FileTree, error) {

	path = filepath.Clean(path)
	path = filepath.ToSlash(path)

	node := t.root
	for {
		var next, rest string
		strs := strings.SplitN(path, "/", 2)
		next = strs[0]
		if len(strs) == 2 {
			next = strs[1]
		}

		// find child
		l := len(node.Children)

		k := sort.Search(l, func(i int) bool {
			return next <= node.Children[i].File.Name()
		})

		if k == l {
			return nil, ErrNodeNotFound
		}

		child := node.Children[k]
		if next != child.File.Name() {
			return nil, ErrNodeNotFound
		}

		if rest == "" {
			child.File = CustomFile(CustomFileArgs{
				Size:               child.File.Size(),
				ModTime:            child.File.ModTime(),
				IsDir:              child.File.IsDir(),
				IsSymlink:          child.File.IsSymlink(),
				IsSymlinkNotCached: !child.File.SymlinkIsCached(),
				Symlink:            child.File.Symlink(),
				Name:               ".",
				ReadCloser:         child.File,
			})
			child.Parent = nil
			subtree := &tree{
				root: child,
			}
			return subtree, nil
		}

		path = rest
		node = child

	}

}

func (t *tree) Unmap(path string) error {

	path = filepath.Clean(path)
	path = filepath.ToSlash(path)
	return t.root.unmap(path)

}
