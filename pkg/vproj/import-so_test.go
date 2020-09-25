package vproj

import (
	"debug/elf"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vorteil/vorteil/pkg/elog"
)

func TestNewImportSharedObject(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Logf("Import Shared Objects Only Supported on Linux")
		return
	}

	// Invalid Test - No existing dir
	isoOp, err := NewImportSharedObject("/tmp/no-existent-Path", true, &elog.CLI{})
	assert.Error(t, err)
	assert.Nil(t, isoOp)

	// Invalid Test - File not dir
	emptyFile, err := ioutil.TempFile(os.TempDir(), "vorteil-iso-test")
	assert.NoError(t, err, "Could not create tmp file for testing")
	defer os.Remove(emptyFile.Name())
	isoOp, err = NewImportSharedObject(emptyFile.Name(), true, &elog.CLI{})
	assert.Error(t, err)
	assert.Nil(t, isoOp)

	// Valid Test - Empty Directory
	dir, err := ioutil.TempDir(os.TempDir(), "vorteil-iso-test")
	assert.NoError(t, err, "Could not create tmp dir for testing")
	defer os.RemoveAll(dir)
	isoOp, err = NewImportSharedObject(dir, true, &elog.CLI{})
	assert.NoError(t, err)
	assert.NotNil(t, isoOp)
}

func TestGetProjectFiles(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Logf("Import Shared Objects Only Supported on Linux")
		return
	}
	// Create Temp Directory
	dir, err := ioutil.TempDir(os.TempDir(), "vorteil-iso-test")
	assert.NoError(t, err, "Could not create tmp dir for testing")
	defer os.RemoveAll(dir)
	err = copyGoBinaryToDir(dir)
	if !assert.NoError(t, err, "Could not copy Go binary to Temp Path: "+dir) {
		os.Exit(1)
	}

	isoOp, err := NewImportSharedObject(dir, true, &elog.CLI{})
	assert.NoError(t, err)
	if assert.NotNil(t, isoOp) {
		projectPaths, err := isoOp.getProjectFiles()
		if assert.NoError(t, err) && assert.NotEmpty(t, projectPaths) {
			assert.Equal(t, projectPaths[0], filepath.Join(dir, "/go"))
		}
	}
}

func TestGetFindLibAndOpenElfAndGetLibraries(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Logf("Import Shared Objects Only Supported on Linux")
		return
	}
	// Create Temp Directory
	dir, err := ioutil.TempDir(os.TempDir(), "vorteil-iso-test")
	assert.NoError(t, err, "Could not create tmp dir for testing")
	defer os.RemoveAll(dir)

	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fail()
	}

	e, eLibs, err := openElfAndGetLibraries(goPath)
	if !assert.NoError(t, err) {
		t.Fail()
	}
	defer e.Close()

	if !assert.NotEmpty(t, eLibs) {
		t.Fail()
	}

	isoOp, err := NewImportSharedObject(dir, true, &elog.CLI{})
	assert.NoError(t, err)
	if assert.NotNil(t, isoOp) {
		libPath, found, err := isoOp.findLib(eLibs[0], elf.ELFCLASS64)
		assert.NoError(t, err)
		assert.True(t, found)
		assert.NotEmpty(t, libPath)

		// Find lib that does not exist
		libPath, found, err = isoOp.findLib("lib-does-not-exists", elf.ELFCLASS64)
		assert.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, libPath)
	}
}

func TestImportSharedObjectsOperation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Logf("Import Shared Objects Only Supported on Linux")
		return
	}
	// Create Temp Directory
	dir, err := ioutil.TempDir(os.TempDir(), "vorteil-iso-test")
	assert.NoError(t, err, "Could not create tmp dir for testing")
	defer os.RemoveAll(dir)
	err = copyGoBinaryToDir(dir)
	if !assert.NoError(t, err, "Could not copy Go binary to Temp Path: "+dir) {
		os.Exit(1)
	}

	isoOp, err := NewImportSharedObject(dir, false, &elog.CLI{})
	assert.NoError(t, err)
	if assert.NotNil(t, isoOp) {
		err := isoOp.Start()
		assert.NoError(t, err)
	}
}

func copyGoBinaryToDir(dstDir string) error {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return err
	}

	in, err := os.Open(goPath)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(filepath.Join(dstDir, "go"))
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Close()
}
