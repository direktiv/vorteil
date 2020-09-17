package vconvert

import (
	"io/ioutil"
	"os"

	// "path/filepath"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/vorteil/vorteil/pkg/elog"
)

func TestNewContainerConverter(t *testing.T) {

	var cc = []struct {
		app     string
		rtype   RegistryType
		success bool
	}{
		{"", NullRegistry, false},
		{"local.docker/myapp", LocalRegistry, true},
		{"local.containerd/myapp", LocalRegistry, true},
		{"local.unknown/myapp", NullRegistry, false},
		{"tomcat", RemoteRegistry, true},
		{"myrepo.io/tomcat", RemoteRegistry, true},
		{"myrepo.io/tomcat:latest", RemoteRegistry, true},
		{"myrepo.io/tomcat:latest/jens", NullRegistry, false},
	}

	for _, c := range cc {
		r, err := NewContainerConverter(c.app, nil)
		if c.success {
			assert.NoError(t, err)
			assert.Equal(t, r.registryType, c.rtype)
		} else {
			assert.Error(t, err)
		}
	}

}

func TestBlobDownloadWorker(t *testing.T) {

	r, _ := NewContainerConverter("hello-world", nil)
	err := r.downloadImageInformation(&RegistryConfig{
		URL: "https://registry-1.docker.io",
	})
	assert.NoError(t, err)
	assert.NotNil(t, r.layers[0])

	dir, _ := ioutil.TempDir("", "vtest")
	j := job{
		layer: r.layers[0],
		dir:   dir,
	}
	defer os.RemoveAll(dir)

	go r.blobDownloadWorker()
	r.jobsCh <- &j
	<-r.jobsDoneCh

	assert.NoError(t, j.err)
	assert.FileExists(t, j.name)

	j.dir = "/does/not/exist"
	j.name = ""

	r.jobsCh <- &j
	<-r.jobsDoneCh

	assert.Error(t, j.err)
	assert.NoFileExists(t, j.name)

}

func TestCreateVCFG(t *testing.T) {

	var config v1.Config
	config.WorkingDir = "/"

	r := &ContainerConverter{
		logger: &elog.CLI{},
	}
	err := r.createVCFG(config, "/does/not/exist")
	assert.Error(t, err)

	dir, _ := ioutil.TempDir("", "vtest")
	defer os.RemoveAll(dir)

	// no command error
	err = r.createVCFG(config, dir)
	assert.Error(t, err)

	// config.WorkingDir = "/testdir"
	// config.WorkingDir = "/testdir"

	config.Entrypoint = []string{"ep"}
	config.Cmd = []string{"bin"}

	err = r.createVCFG(config, dir)
	assert.NoError(t, err)

}

func TestDownloadBlobs(t *testing.T) {

	dir, _ := ioutil.TempDir("", "vtest")
	defer os.RemoveAll(dir)

	r, _ := NewContainerConverter("hello-world", nil)
	err := r.downloadImageInformation(&RegistryConfig{
		URL: "https://registry-1.docker.io",
	})
	assert.NoError(t, err)

	err = r.downloadBlobs("")
	assert.Error(t, err)

	err = r.downloadBlobs(dir)
	assert.NoError(t, err)

}

func TestUntarLayers(t *testing.T) {

	r, _ := NewContainerConverter("hello-world", nil)

	// not allowed
	err := r.untarLayers("/dev")
	assert.Error(t, err)

	// not empty test
	err = r.untarLayers("../../test/vconvert")
	assert.Error(t, err)

	r.layers = make([]*layer, 1)
	r.layers[0] = &layer{
		file: "",
	}

	dir, _ := ioutil.TempDir("", "vtest")
	defer os.RemoveAll(dir)

	err = r.untarLayers(dir)
	assert.Error(t, err)

	r.layers[0] = &layer{
		file: "../../test/vconvert/123layer.tar",
	}

	err = r.untarLayers(dir)
	assert.NoError(t, err)

	// there should be only one file in the dir
	files, _ := ioutil.ReadDir(dir)
	assert.Equal(t, 1, len(files))

}
