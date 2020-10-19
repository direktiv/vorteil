package registry

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/provisioners"
	"github.com/vorteil/vorteil/pkg/provisioners/amazon"
	"github.com/vorteil/vorteil/pkg/provisioners/azure"
	"github.com/vorteil/vorteil/pkg/provisioners/google"
)

func init() {

	fn := func(log elog.View, data []byte) (provisioners.Provisioner, error) {
		return google.NewProvisioner(&google.ProvisionerArgs{
			Logger: log,
			Data:   data,
		})
	}

	err := RegisterProvisioner(google.ProvisionerType, fn)
	if err != nil {
		panic(err)
	}

	err = RegisterProvisioner(amazon.ProvisionerType, fn)
	if err != nil {
		panic(err)
	}

	err = RegisterProvisioner(azure.ProvisionerType, fn)
	if err != nil {
		panic(err)
	}
}

// ProvisionerInstantiator - TODO:
type ProvisionerInstantiator func(log elog.View, data []byte) (provisioners.Provisioner, error)

var registeredProvisioners map[string]ProvisionerInstantiator

// RegisterProvisioner - TODO:
func RegisterProvisioner(name string, fn ProvisionerInstantiator) error {

	if registeredProvisioners == nil {
		registeredProvisioners = make(map[string]ProvisionerInstantiator)
	}

	if _, exists := registeredProvisioners[name]; exists {
		return fmt.Errorf("refusing to register provisioner '%s': already registered", name)
	}

	registeredProvisioners[name] = fn
	return nil
}

// NewProvisioner ...
func NewProvisioner(name string, log elog.View, data []byte) (provisioners.Provisioner, error) {

	fn, exists := registeredProvisioners[name]
	if !exists {
		return nil, fmt.Errorf("provisioner '%s' not found", name)
	}

	return fn(log, data)
}
