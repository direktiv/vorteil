package vconvert

// import (
// 	"io/ioutil"
// 	"os"
// 	"testing"
//
// 	"github.com/stretchr/testify/assert"
// 	"github.com/vorteil/vorteil/pkg/elog"
// )
//
// // for these tests to run docker needs to be installed
// // and the hello-world image needs to be pulled by docker and containerd
// // docker pull hello-world
// // ctr images pull docker.io/library/hello-world:latest
// // the user needs to be in the docker and root group
// // sudo usermod -a -G root,docker USER
// func TestLocalDocker(t *testing.T) {
//
// 	f, _ := ioutil.TempDir("", "vtest")
// 	defer os.Remove(f)
//
// 	r, _ := newHandler("hello-world", "", "", f)
// 	r.fetchReader = localGetReader
//
// 	var tts = []struct {
// 		name, tag  string
// 		successful bool
// 	}{
// 		{"hello-world", "latest", true},
// 		{"doesnot", "exist", false},
// 	}
//
// 	for _, tt := range tts {
// 		err := r.downloadDockerTar(tt.name, tt.tag)
// 		if tt.successful {
// 			assert.NoError(t, err)
// 		} else {
// 			assert.Error(t, err)
// 		}
// 	}
// }
//
// func TestLocalContainerd(t *testing.T) {
// 	f, _ := ioutil.TempDir("", "vtest")
// 	defer os.Remove(f)
//
// 	r, _ := newHandler("hello-world", "", "", f)
// 	r.fetchReader = localGetReader
//
// 	var ttsc = []struct {
// 		name, tag  string
// 		successful bool
// 	}{
// 		{"docker.io/library/hello-world", "latest", true},
// 		{"doesnot", "exist", false},
// 	}
//
// 	for _, tt := range ttsc {
// 		err := r.downloadContainerdTar(tt.name, tt.tag)
// 		if tt.successful {
// 			assert.NoError(t, err)
// 		} else {
// 			assert.Error(t, err)
// 		}
// 	}
//
// }
//
// func TestLocal(t *testing.T) {
//
// 	elog.IsJSON = true
// 	initConfig("")
//
// 	f, _ := ioutil.TempDir("", "vtest")
// 	defer os.Remove(f)
//
// 	r, _ := newHandler("hello-world", "", "", f)
// 	r.fetchReader = localGetReader
//
// 	var ttsc = []struct {
// 		name, tag  string
// 		successful bool
// 	}{
// 		{"docker.io/library/hello-world", "latest", true},
// 		{"doesnot", "exist", false},
// 	}
//
// 	for _, tt := range ttsc {
// 		err := r.downloadContainerdTar(tt.name, tt.tag)
// 		if tt.successful {
// 			assert.NoError(t, err)
// 		} else {
// 			assert.Error(t, err)
// 		}
// 	}
//
// }
//
// func TestLocalNotSet(t *testing.T) {
//
// 	r := imageHandler{}
// 	err := r.downloadDockerTar("", "")
// 	assert.Error(t, err)
//
// 	err = r.downloadContainerdTar("", "")
// 	assert.Error(t, err)
//
// }
