package vconvert

import (
	// "io/ioutil"
	// "os"

	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDownloadImageInformationConfig(t *testing.T) {

	r := &ContainerConverter{}

	// should all fail
	var cc = []struct {
		config *RegistryConfig
	}{
		{nil},
		{&RegistryConfig{}},
	}

	for _, c := range cc {
		err := r.downloadInformationRemote(c.config)
		assert.Error(t, err)
	}

	err := r.DownloadImageInformation(nil)
	assert.Error(t, err)

}

func TestDownloadImageInformation(t *testing.T) {

	testFile := "../../test/vconvert/deleteme.tar"
	defer os.Remove(testFile)

	// create instance
	f, _ := ioutil.TempDir("", "vtest")
	defer os.Remove(f)
	r, _ := NewContainerConverter("hello-world", f)

	err := r.DownloadImageInformation(&RegistryConfig{
		URL: "https://registry-1.docker.io",
	})
	assert.NoError(t, err)

}
