package vconvert

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

}
