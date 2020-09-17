package vconvert

import (
	// "io/ioutil"
	// "os"

	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDownloadInformationRemoteFailure(t *testing.T) {

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

	err := r.downloadImageInformation(nil)
	assert.Error(t, err)

}

func TestDownloadInformationRemoteSuccess(t *testing.T) {

	r, _ := NewContainerConverter("hello-world", nil)

	err := r.downloadImageInformation(&RegistryConfig{
		URL: "https://registry-1.docker.io",
	})
	assert.NoError(t, err)

}
