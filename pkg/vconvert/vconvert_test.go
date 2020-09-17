package vconvert

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitConverter(t *testing.T) {

	dir, _ := ioutil.TempDir("", "test")
	defer os.Remove(dir)

	// parsing should fail
	_, err := NewContainerConverter("", "")
	assert.Error(t, err)

	c, err := NewContainerConverter("local.docker/myapp", dir)
	assert.NoError(t, err)
	assert.Equal(t, c.RegistryType, LocalRegistry)

	c, err = NewContainerConverter("local.containerd/myapp", dir)
	assert.NoError(t, err)
	assert.Equal(t, c.RegistryType, LocalRegistry)

	c, err = NewContainerConverter("local.unknown/myapp", dir)
	assert.Error(t, err)

	c, err = NewContainerConverter("tomcat", dir)
	assert.NoError(t, err)
	assert.Equal(t, c.RegistryType, RemoteRegistry)

	c, err = NewContainerConverter("myrepo.io/tomcat", dir)
	assert.NoError(t, err)
	assert.Equal(t, "myrepo.io", c.RegistryName())

}
