package vio

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	unixpath "path"
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

func (n *TreeNode) Path() string {

	if n.Parent == nil || n.Parent == n {
		return "/"
	}

	s := n.Parent.Path()
	return unixpath.Join(s, n.File.Name())

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

	if n.Path() == "/" {
		return "."
	}

	return "." + n.Path()

}

func splitPath(path string) (next, rest string) {

	strs := strings.SplitN(path, "/", 2)
	next = strs[0]
	if len(strs) == 2 {
		rest = strs[1]
	}

	return next, rest

}

func (n *TreeNode) mapIn(path string, f File) error {

	var err error
	next, rest := splitPath(path)

	newNode := &TreeNode{
		Parent:   n,
		Children: []*TreeNode{},
	}

	if rest == "" {
		newNode.File = f
	} else {
		newNode.File = CustomFile(CustomFileArgs{
			Name:    next,
			IsDir:   true,
			ModTime: f.ModTime(),
		})
		err = newNode.mapIn(rest, f)
		if err != nil {
			return err
		}
	}

	before, selected, after := n.sliceChildren(next)

	if selected != nil {
		if selected.File.IsDir() && newNode.File.IsDir() {
			// merge
			if rest != "" {
				err = selected.mapIn(rest, f)
			}
			return err
		}

		// replace
		err := selected.close()
		if err != nil {
			return err
		}
	}

	// insert
	n.Children = append(before, append([]*TreeNode{newNode}, after...)...)
	return nil

}

func (n *TreeNode) mergeTree(children []*TreeNode) error {

	for _, child := range children {

		err := n.mapInSubTree(child.File.Name(), &tree{
			root: child,
		})
		if err != nil {
			return err
		}

	}

	return nil

}

func (n *TreeNode) mapInSubTree(path string, sub FileTree) error {

	var err error
	next, rest := splitPath(path)

	var newNode *TreeNode

	if rest == "" {
		newNode = sub.(*tree).root
	} else {
		newNode = &TreeNode{
			Parent:   n,
			Children: []*TreeNode{},
			File: CustomFile(CustomFileArgs{
				Name:    next,
				IsDir:   true,
				ModTime: sub.(*tree).root.File.ModTime(),
			}),
		}
		err = newNode.mapInSubTree(rest, sub)
		if err != nil {
			return err
		}
	}

	before, selected, after := n.sliceChildren(next)

	if selected != nil {
		// merge
		if selected.File.IsDir() && newNode.File.IsDir() {
			return n.mergeTree(newNode.Children)
		}

		// replace
		err := selected.close()
		if err != nil {
			return err
		}
	}

	// insert
	n.Children = append(before, append([]*TreeNode{newNode}, after...)...)
	return nil

}

func (n *TreeNode) close() error {

	err := n.walk(func(path string, f File) error {
		return f.Close()
	})
	if err != nil {
		return err
	}

	return nil

}

func (n *TreeNode) sliceChildren(next string) (before []*TreeNode, selected *TreeNode, after []*TreeNode) {

	l := len(n.Children)
	k := sort.Search(l, func(i int) bool {
		return next <= n.Children[i].File.Name()
	})

	if k == l || next != n.Children[k].File.Name() {
		return n.Children[:k], nil, n.Children[k:]
	}

	return n.Children[:k], n.Children[k], n.Children[k+1:]

}

func (n *TreeNode) unmap(path string) error {

	var err error

	next, rest := splitPath(path)
	before, selected, after := n.sliceChildren(next)
	if selected == nil {
		return ErrNodeNotFound
	}

	if rest != "" {
		return selected.unmap(rest)
	}

	err = selected.close()
	if err != nil {
		return err
	}

	n.Children = append(before, after...)
	return nil

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

type loadFromDirectory struct {
	dir  string
	tree FileTree
}

func (v *loadFromDirectory) walker(path string, fi os.FileInfo, err error) error {

	path = filepath.ToSlash(path)
	abs := path
	path = strings.TrimPrefix(path, v.dir)
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return nil
	}

	f, err := LazyOpen(abs)
	if err != nil {
		return err
	}

	err = v.tree.Map(path, f)
	if err != nil {
		return err
	}

	return nil

}

// FileTreeFromDirectory creates a new FileTree based on a directory. The
// files in the tree will be loaded in lazily, so the function should be safe
// for use on very large directory trees.
func FileTreeFromDirectory(dir string) (FileTree, error) {

	v := &loadFromDirectory{
		tree: NewFileTree(),
		dir:  dir,
	}

	err := filepath.Walk(dir, v.walker)
	if err != nil {
		return nil, err
	}

	return v.tree, nil

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

func (t *tree) Map(path string, f File) error {

	if f.Size() < 0 {
		return errors.New("cannot map object with negative size")
	}

	path = filepath.ToSlash(path)
	path = unixpath.Clean(path)
	path = filepath.ToSlash(path)
	path = unixpath.Join("/", path)
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return errors.New("cannot map over the root node")
	}

	f = CustomFile(CustomFileArgs{
		Name:               unixpath.Base(path),
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

	path = filepath.ToSlash(path)
	path = unixpath.Clean(path)
	path = filepath.ToSlash(path)

	st := sub.(*tree)
	f := st.root.File
	st.root.File = CustomFile(CustomFileArgs{
		Name:               unixpath.Base(path),
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

	path = unixpath.Clean(path)
	path = filepath.ToSlash(path)

	node := t.root
	for {
		next, rest := splitPath(path)
		_, selected, _ := node.sliceChildren(next)

		if selected == nil {
			return nil, ErrNodeNotFound
		}

		if rest == "" {
			selected.Parent = nil
			subtree := &tree{
				root: selected,
			}
			return subtree, nil
		}

		path = rest
		node = selected

	}

}

func (t *tree) Unmap(path string) error {

	path = unixpath.Clean(path)
	path = filepath.ToSlash(path)
	return t.root.unmap(path)

}
