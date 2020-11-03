package registry

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/provisioners"
	"github.com/vorteil/vorteil/pkg/provisioners/amazon"
	"github.com/vorteil/vorteil/pkg/provisioners/azure"
	"github.com/vorteil/vorteil/pkg/provisioners/google"
)

func init() {

	gcpFn := func(log elog.View, data []byte) (provisioners.Provisioner, error) {
		var cfg google.Config
		err := json.Unmarshal(data, &cfg)
		if err != nil {
			return nil, err
		}
		return google.NewProvisioner(log, &cfg)
	}

	awsFn := func(log elog.View, data []byte) (provisioners.Provisioner, error) {
		var cfg amazon.Config
		err := json.Unmarshal(data, &cfg)
		if err != nil {
			return nil, err
		}
		return amazon.NewProvisioner(log, &cfg)
	}

	azureFn := func(log elog.View, data []byte) (provisioners.Provisioner, error) {
		var cfg azure.Config
		err := json.Unmarshal(data, &cfg)
		if err != nil {
			return nil, err
		}
		return azure.NewProvisioner(log, &cfg)
	}

	err := RegisterProvisioner(google.ProvisionerType, gcpFn)
	if err != nil {
		panic(err)
	}

	err = RegisterProvisioner(amazon.ProvisionerType, awsFn)
	if err != nil {
		panic(err)
	}

	err = RegisterProvisioner(azure.ProvisionerType, azureFn)
	if err != nil {
		panic(err)
	}
}

// ProvisionerInstantiator is a function that returns a new provisioner
type ProvisionerInstantiator func(log elog.View, data []byte) (provisioners.Provisioner, error)

var registeredProvisioners map[string]ProvisionerInstantiator

// RegisterProvisioner registers a ProvisionerInstantiator with a given name.
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

// Provisioners returns an alphabetised list of all registered
// provisioners. Note that a single provisioners may appear multiple times
// in the list under different names.
func Provisioners() []string {

	var names = []string{}

	if registeredProvisioners == nil {
		return names
	}

	for k := range registeredProvisioners {
		names = append(names, k)
	}

	sort.Strings(names)
	return names

}

// DeregisterProvisioner deregisters a ProvisionerInstantiator for the
// given name.
func DeregisterProvisioner(name string) error {

	if registeredProvisioners != nil {
		if _, exists := registeredProvisioners[name]; exists {
			delete(registeredProvisioners, name)
			return nil
		}
	}

	return fmt.Errorf("provisioner '%s' not found", name)

}

// NewProvisioner returns a provisioners.Provisioner object that can be used to
// provision a vorteil buildable, if the named provisioner is registered.
func NewProvisioner(name string, log elog.View, data []byte) (provisioners.Provisioner, error) {

	fn, exists := registeredProvisioners[name]
	if !exists {
		return nil, fmt.Errorf("provisioner '%s' not found", name)
	}

	return fn(log, data)
}
