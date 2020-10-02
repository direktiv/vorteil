package vconvert

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
)

func TestNewContainerConverter(t *testing.T) {

	var cc = []struct {
		app     string
		rtype   RegistryType
		success bool
	}{
		{"", NullRegistry, false},
		{"local.docker/myapp", DockerRegistry, true},
		{"local.containerd/myapp", ContainerdRegistry, true},
		{"local.unknown/myapp", NullRegistry, false},
		{"tomcat", RemoteRegistry, true},
		{"myrepo.io/tomcat", RemoteRegistry, true},
		{"myrepo.io/tomcat:latest", RemoteRegistry, true},
		{"myrepo.io/tomcat:latest/jens", NullRegistry, false},
	}

	for _, c := range cc {
		r, err := NewContainerConverter(c.app, "", nil)
		if c.success {
			assert.NoError(t, err)
			assert.Equal(t, r.RegistryType(), c.rtype)
		} else {
			assert.Error(t, err)
		}
	}

	r, err := NewContainerConverter("tomcat", "", nil)
	assert.NoError(t, err)
	assert.Equal(t, r.RegistryName(), "docker.io")

	r, err = NewContainerConverter("myrepo.io/tomcat", "", nil)
	assert.NoError(t, err)
	assert.Equal(t, r.RegistryName(), "myrepo.io")

}

func TestBlobDownloadWorker(t *testing.T) {

	r, _ := NewContainerConverter("hello-world", "", nil)
	err := r.downloadImageInformation(&registryConfig{
		url: "https://registry-1.docker.io",
	})
	assert.NoError(t, err)
	assert.NotNil(t, r.layers[0])

	dir, _ := ioutil.TempDir("", "vtest")
	j := job{
		layer: r.layers[0],
		dir:   dir,
	}
	defer os.RemoveAll(dir)

	r.jobsCh = make(chan *job, len(r.layers))

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

	// no command error
	err = r.createVCFG(config, "../../test/vconvert/")
	assert.Error(t, err)

	config.Cmd = []string{"bin", "run", "smooth"}

	// can not find binary
	err = r.createVCFG(config, "../../test/vconvert/")
	assert.Error(t, err)

	// cwd works
	config.WorkingDir = "/find"
	err = r.createVCFG(config, "../../test/vconvert/")
	assert.NoError(t, err)
	defer os.Remove("../../test/vconvert/default.vcfg")
	defer os.Remove("../../test/vconvert/.vorteilproject")

	// pathy works
	config.WorkingDir = "/"
	config.Env = []string{"PATH=/find"}

	type dummy struct{}
	ep := make(map[string]struct{})
	ep["8080/tcp"] = dummy{}
	ep["9090/udp"] = dummy{}
	ep["6969"] = dummy{}

	config.ExposedPorts = ep

	err = r.createVCFG(config, "../../test/vconvert/")
	assert.NoError(t, err)

	v, _ := ioutil.ReadFile("../../test/vconvert/default.vcfg")
	vf := new(vcfg.VCFG)
	vf.Load(v)

	assert.Equal(t, len(vf.Networks[0].TCP), 2)
	assert.Equal(t, "9090", vf.Networks[0].UDP[0])

	assert.Equal(t, vf.Programs[0].Args, "/find/bin run smooth")
	assert.Equal(t, len(vf.Programs[0].Env), 1)
}

func TestDownloadBlobs(t *testing.T) {

	dir, _ := ioutil.TempDir("", "vtest")
	defer os.RemoveAll(dir)

	r, _ := NewContainerConverter("hello-world", "", nil)
	err := r.downloadImageInformation(&registryConfig{
		url: "https://registry-1.docker.io",
	})
	assert.NoError(t, err)

	err = r.downloadBlobs("")
	assert.Error(t, err)

	err = r.downloadBlobs(dir)
	assert.NoError(t, err)

}

func TestUntarLayers(t *testing.T) {

	r, _ := NewContainerConverter("hello-world", "", nil)

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

func TestConvertToProject(t *testing.T) {

	r, _ := NewContainerConverter("hello-world", "", nil)

	err := r.ConvertToProject("../../test/vconvert", "", "")
	assert.Error(t, err)

	dir, _ := ioutil.TempDir("", "vtest")
	defer os.RemoveAll(dir)

	err = r.ConvertToProject(dir, "", "")
	assert.NoError(t, err)

	assert.FileExists(t, filepath.Join(dir, "default.vcfg"))
	assert.FileExists(t, filepath.Join(dir, ".vorteilproject"))

}
