package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/vorteil/vorteil/pkg/vpkg"
)

func TestHandleFileInjections(t *testing.T) {

	b := vpkg.NewBuilder()

	var testFileNotExists, testDirNotExists string

	// create file and dir for 'existing' tests
	f, err := ioutil.TempFile(os.TempDir(), "vorteil-test-")
	if err != nil {
		t.Fatal(err.Error())
	}
	defer os.Remove(f.Name())

	dir, err := ioutil.TempDir(os.TempDir(), "vorteil-test-")
	if err != nil {
		t.Fatal(err.Error())
	}
	defer os.RemoveAll(dir)

	filesMap[f.Name()] = []string{"/test1"}
	filesMap[dir] = []string{"/test2"}

	err = handleFileInjections(b)
	if err != nil {
		t.Fatal(err.Error())
	}

	delete(filesMap, f.Name())
	delete(filesMap, dir)

	testFileNotExists = filepath.Join(f.Name(), "test")
	testDirNotExists = filepath.Join(dir, "test")

	filesMap[testFileNotExists] = []string{"/test3"}
	filesMap[testDirNotExists] = []string{"/test4"}

	err = handleFileInjections(b)
	if err == nil {
		t.Fatal("expected failure; source file/dir do not exist")
	}

}
