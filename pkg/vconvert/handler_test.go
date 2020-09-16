package vconvert

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vorteil/vorteil/pkg/vcfg"
)

func TestParse(t *testing.T) {
	_, err := newHandler("", "", "", "/does/no/exist")
	assert.Error(t, err)
}

type dummy struct {
}

func TestVCFGFound(t *testing.T) {

	r := &imageHandler{}

	defer os.Remove("../../test/vconvert/.vorteilproject")
	defer os.Remove("../../test/vconvert/default.vcfg")

	r.imageConfig.Entrypoint = []string{}
	r.imageConfig.Cmd = []string{"/find/bin"}
	r.imageConfig.WorkingDir = "/"

	ep := make(map[string]struct{})
	ep["8080/tcp"] = dummy{}
	ep["9090/udp"] = dummy{}
	ep["6969"] = dummy{}

	r.imageConfig.ExposedPorts = ep

	err := r.createVCFG("../../test/vconvert")
	assert.NoError(t, err)

	v, _ := ioutil.ReadFile("../../test/vconvert/default.vcfg")

	vf := new(vcfg.VCFG)
	vf.Load(v)

	assert.Equal(t, "8080", vf.Networks[0].TCP[0])
	assert.Equal(t, "6969", vf.Networks[0].TCP[1])
	assert.Equal(t, "9090", vf.Networks[0].UDP[0])

}

func TestVCFGNotFound(t *testing.T) {

	f, _ := ioutil.TempDir("", "vtest")
	defer os.Remove(f)
	r := &imageHandler{}

	r.imageConfig.Entrypoint = []string{"ep"}
	r.imageConfig.Cmd = []string{"cmd"}
	r.imageConfig.WorkingDir = "/wd"

	err := r.createVCFG(f)
	assert.Error(t, err)
}

func TestLocalContainerdConvert(t *testing.T) {

	initConfig("")

	f, _ := ioutil.TempDir("", "vtest")
	defer os.Remove(f)

	r, err := newHandler("local.containerd/docker.io/library/hello-world:latest", "", "", f)
	assert.NoError(t, err)

	err = r.createTar()
	assert.NoError(t, err)

}

func TestLocalDockerConvert(t *testing.T) {

	initConfig("")

	f, _ := ioutil.TempDir("", "vtest")
	defer os.Remove(f)

	r, err := newHandler("local.docker/hello-world", "", "", f)
	assert.NoError(t, err)

	err = r.createTar()
	assert.NoError(t, err)

}

func TestLocalDockerFail(t *testing.T) {

	initConfig("")

	f, _ := ioutil.TempDir("", "vtest")
	defer os.Remove(f)

	r, err := newHandler("local.nope/hello-world", "", "", f)
	assert.NoError(t, err)

	err = r.createTar()
	assert.Error(t, err)

}

func TestRemoteConvert(t *testing.T) {

	initConfig("")

	f, _ := ioutil.TempDir("", "vtest")
	defer os.Remove(f)

	r, err := newHandler("hello-world", "", "", f)
	assert.NoError(t, err)

	err = r.createTar()
	assert.NoError(t, err)

	// downloaded layer
	files, _ := ioutil.ReadDir(r.tmpDir)
	assert.Equal(t, 1, len(files))

	err = r.untarLayers(f)
	assert.NoError(t, err)

	// the destination folder should have one file now as well
	files, _ = ioutil.ReadDir(r.tmpDir)
	assert.Equal(t, 1, len(files))

	// there should be hello, .vorteilproject and default.vcfg in the dir
	r.createVCFG(f)
	files, _ = ioutil.ReadDir(f)
	assert.Equal(t, 3, len(files))

}
