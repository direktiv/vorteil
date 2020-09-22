package vproj

import (
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/vorteil/vorteil/pkg/elog"

	"github.com/stretchr/testify/assert"
)

func TestSharedObjectInitializationInvalid(t *testing.T) {
	isoOp, err := NewImportSharedObject("/tmp/no-existent-Path", true, &elog.CLI{})
	assert.Error(t, err)
	assert.Nil(t, isoOp)
}

// func TestSharedObjectInitializationValid(t *testing.T) {
// 	// SETUP
// 	dir, err := ioutil.TempDir(os.TempDir(), "vorteil-iso-test")
// 	assert.NoError(t, err, "Could not create tmp dir for testing")
// 	defer os.RemoveAll(dir)
// 	err = copyGoBinaryToDir(dir)
// 	assert.NoError(t, err, "Could not copy Go binary to Temp Path: "+dir)

// 	isoOp, err := NewImportSharedObject(dir, true, &elog.CLI{})
// 	assert.NoError(t, err)
// 	assert.NotNil(t, isoOp)
// }

// func TestSharedObjectInitializationStart(t *testing.T) {
// 	// SETUP
// 	dirExclude, err := ioutil.TempDir(os.TempDir(), "vorteil-iso-test")
// 	assert.NoError(t, err, "Could not create tmp dir for testing")
// 	defer os.RemoveAll(dirExclude)
// 	err = copyGoBinaryToDir(dirExclude)
// 	assert.NoError(t, err, "Could not copy Go binary to Temp Path: "+dirExclude)

// 	dir, err := ioutil.TempDir(os.TempDir(), "vorteil-iso-test")
// 	assert.NoError(t, err, "Could not create tmp dir for testing")
// 	defer os.RemoveAll(dir)
// 	err = copyGoBinaryToDir(dir)
// 	assert.NoError(t, err, "Could not copy Go binary to Temp Path: "+dir)

// 	isoOp, err := NewImportSharedObject(dir, false, &elog.CLI{})
// 	assert.NoError(t, err)
// 	assert.NotNil(t, isoOp)

// 	err = isoOp.Start()
// 	assert.NoError(t, err)

// 	isoOpExclude, err := NewImportSharedObject(dirExclude, true, &elog.CLI{})
// 	assert.NoError(t, err)
// 	assert.NotNil(t, isoOpExclude)

// 	err = isoOpExclude.Start()
// 	assert.NoError(t, err)
// }

// func TestSharedObjectFindLib(t *testing.T) {
// 	dir, err := ioutil.TempDir(os.TempDir(), "vorteil-iso-test")
// 	assert.NoError(t, err, "Could not create tmp dir for testing")
// 	defer os.RemoveAll(dir)
// 	err = copyGoBinaryToDir(dir)
// 	assert.NoError(t, err, "Could not copy Go binary to Temp Path: "+dir)

// 	isoOp, err := NewImportSharedObject(dir, true, &elog.CLI{})
// 	assert.NoError(t, err)
// 	assert.NotNil(t, isoOp)

// 	goPath, err := exec.LookPath("go")
// 	assert.NoError(t, err, "Could not locate Go binary on system")

// 	eGo, err := elf.Open(goPath)
// 	assert.NoError(t, err, "Could not open Go binary on system")
// 	defer eGo.Close()

// 	goDeps, err := eGo.ImportedLibraries()
// 	assert.NoError(t, err, "Could not get Go binary dependencies")
// 	assert.Greater(t, len(goDeps), 0, "Go binary has no dependencies")

// 	for _, goDep := range goDeps {
// 		path, err := isoOp.findLib(goDep, eGo.Class)
// 		assert.NotEmpty(t, path)
// 		assert.NoError(t, err)
// 	}

// 	// Invalid Lib
// 	path, err := isoOp.findLib("FAKE.LIB.NAME", elf.ELFCLASS64)
// 	assert.Empty(t, path)
// 	assert.Error(t, err)
// }

func TestSharedObjectReadLink(t *testing.T) {
	// SETUP
	emptyFile, err := ioutil.TempFile(os.TempDir(), "vorteil-iso-test")
	assert.NoError(t, err, "Could not create tmp file for testing")
	defer os.Remove(emptyFile.Name())

	err = os.Symlink(emptyFile.Name(), emptyFile.Name()+".Symlink")
	assert.NoError(t, err, "Could not create symlink: "+emptyFile.Name()+".Symlink")

	// Invalid
	path, err := ReadLink(emptyFile.Name())
	assert.Empty(t, path)
	assert.Error(t, err)

	// Valid
	targetPath, err := ReadLink(emptyFile.Name() + ".Symlink")
	assert.Equal(t, targetPath, emptyFile.Name())
	assert.NoError(t, err)

}

// func TestSharedObjectFindDeps(t *testing.T) {
// 	// SETUP
// 	emptyFile, err := ioutil.TempFile(os.TempDir(), "vorteil-iso-test")
// 	assert.NoError(t, err, "Could not create tmp file for testing")
// 	defer os.Remove(emptyFile.Name())

// 	dir, err := ioutil.TempDir(os.TempDir(), "vorteil-iso-test")
// 	assert.NoError(t, err, "Could not create tmp dir for testing")
// 	defer os.RemoveAll(dir)
// 	err = copyGoBinaryToDir(dir)
// 	assert.NoError(t, err, "Could not copy Go binary to Temp Path: "+dir)

// 	goPath, err := exec.LookPath("go")
// 	assert.NoError(t, err, "Could not locate Go binary on system")

// 	eGo, err := elf.Open(goPath)
// 	assert.NoError(t, err, "Could not open Go binary on system")
// 	defer eGo.Close()

// 	goDeps, err := eGo.ImportedLibraries()
// 	assert.NoError(t, err, "Could not get Go binary dependencies")
// 	assert.Greater(t, len(goDeps), 0, "Go binary has no dependencies")

// 	isoOp, err := NewImportSharedObject(dir, true, &elog.CLI{})
// 	assert.NoError(t, err)
// 	assert.NotNil(t, isoOp)

// 	soPath, err := isoOp.findLib(goDeps[0], eGo.Class)
// 	assert.NoError(t, err, "Could not find path of shared object: "+goDeps[0])

// 	eSOPath, err := elf.Open(soPath)
// 	assert.NoError(t, err)
// 	defer eSOPath.Close()

// 	soDeps, err := eSOPath.ImportedLibraries()
// 	assert.NoError(t, err)
// 	assert.Greater(t, len(soDeps), 0)

// 	isoOperationSODeps, _, err := isoOp.listDependencies(soPath)
// 	assert.NoError(t, err)

// 	for _, isoDep := range isoOperationSODeps {
// 		var found bool
// 		var isoDepName = filepath.Base(isoDep)

// 		// Check if Shared object found with listDependencies exists in slice of shared objects found with ImportedLibraries()
// 		for _, soDep := range soDeps {
// 			if isoDepName == soDep {
// 				found = true
// 				break
// 			}
// 		}

// 		assert.True(t, found, fmt.Sprintf("Shared Object %s was missed by listDependencies function", isoDep))
// 	}

// 	// Invalid Deps
// 	_, _, err = isoOp.listDependencies("/FAKE/PATH/TO/NOTHING")
// 	assert.Error(t, err)

// 	// No Deps
// 	noDeps, _, err := isoOp.listDependencies(emptyFile.Name())
// 	assert.Empty(t, noDeps)
// 	assert.NoError(t, err)
// }

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
