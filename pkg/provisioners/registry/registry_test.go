package registry

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/provisioners"
	"github.com/vorteil/vorteil/pkg/provisioners/azure"
)

func TestProvisioners(t *testing.T) {
	// init should register three provisioners
	assert.Greater(t, len(Provisioners()), 0)
}

func TestRegisterProvisioner(t *testing.T) {
	// init should register three provisioners
	azureFn := func(log elog.View, data []byte) (provisioners.Provisioner, error) {
		var cfg azure.Config
		err := json.Unmarshal(data, &cfg)
		if err != nil {
			return nil, err
		}
		return azure.NewProvisioner(log, &cfg)
	}

	if !assert.NoError(t, RegisterProvisioner("testProvisioner", azureFn)) {
		os.Exit(1)
	}

	assert.Contains(t, Provisioners(), "testProvisioner")
}

func TestDeregisterProvisioner(t *testing.T) {
	// init should register three provisioners
	azureFn := func(log elog.View, data []byte) (provisioners.Provisioner, error) {
		var cfg azure.Config
		err := json.Unmarshal(data, &cfg)
		if err != nil {
			return nil, err
		}
		return azure.NewProvisioner(log, &cfg)
	}

	if !assert.NoError(t, RegisterProvisioner("testProvisioner", azureFn)) {
		os.Exit(1)
	}

	if !assert.NoError(t, DeregisterProvisioner("testProvisioner")) {
		os.Exit(2)
	}

	assert.NotContains(t, Provisioners(), "testProvisioner")
}
