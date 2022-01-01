package ext4

import (
	"context"
	"io"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/vorteil/vorteil/pkg/vio"
)

func TestMinimalFS(t *testing.T) {

	tree := vio.NewFileTree()

	c := NewCompiler(&CompilerArgs{
		FileTree: tree,
	})

	err := c.Commit(context.Background())
	if err != nil {
		t.Errorf("failed to commit minimal fs: %v", err)
	}

	err = c.Precompile(context.Background(), c.MinimumSize())
	if err != nil {
		t.Errorf("failed to precompile minimal fs: %v", err)
	}

	w, err := vio.WriteSeeker(ioutil.Discard)
	if err != nil {
		t.Errorf("failed to precompile minimal fs: %v", err)
	}

	err = c.Compile(context.Background(), w)
	if err != nil {
		t.Errorf("failed to precompile minimal fs: %v", err)
	}

}

func TestTinyFS(t *testing.T) {

	tree := vio.NewFileTree()
	err := tree.Map("/etc", vio.CustomFile(vio.CustomFileArgs{
		Name:  "etc",
		IsDir: true,
	}))
	if err != nil {
		t.Error(err)
	}
	size := 0x30000000
	err = tree.Map("/binary", vio.CustomFile(vio.CustomFileArgs{
		Name:       "binary",
		Size:       size,
		ReadCloser: ioutil.NopCloser(io.LimitReader(vio.Zeroes, int64(size))),
	}))
	if err != nil {
		t.Error(err)
	}

	c := NewCompiler(&CompilerArgs{
		FileTree: tree,
	})

	err = c.Mkdir("/tmp")
	if err != nil {
		t.Errorf("failed to mkdir on tiny fs: %v", err)
	}

	err = c.Mkdir("/sys")
	if err != nil {
		t.Errorf("failed to mkdir on tiny fs: %v", err)
	}

	err = c.Mkdir("/dev")
	if err != nil {
		t.Errorf("failed to mkdir on tiny fs: %v", err)
	}

	data := "vorteil"
	err = c.AddFile("/etc/hosts", ioutil.NopCloser(strings.NewReader(data)), int64(len(data)), true)
	if err != nil {
		t.Errorf("failed to mkdir on tiny fs: %v", err)
	}

	c.IncreaseMinimumInodes(1024)
	c.SetMinimumInodes(512)
	c.SetMinimumInodesPer64MiB(128)
	c.IncreaseMinimumFreeSpace(0x800000)

	err = c.Commit(context.Background())
	if err != nil {
		t.Errorf("failed to commit minimal fs: %v", err)
	}

	err = c.Precompile(context.Background(), c.MinimumSize())
	if err != nil {
		t.Errorf("failed to precompile minimal fs: %v", err)
	}

	w, err := vio.WriteSeeker(ioutil.Discard)
	if err != nil {
		t.Errorf("failed to precompile minimal fs: %v", err)
	}

	err = c.Compile(context.Background(), w)
	if err != nil {
		t.Errorf("failed to precompile minimal fs: %v", err)
	}

}
