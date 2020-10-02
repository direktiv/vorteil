package vconvert

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDownloadInformationRemoteFailure(t *testing.T) {

	r := &ContainerConverter{}

	// should all fail
	var cc = []struct {
		config *registryConfig
	}{
		{nil},
		{&registryConfig{}},
	}

	for _, c := range cc {
		err := r.downloadInformationRemote(c.config)
		assert.Error(t, err)
	}

	err := r.downloadImageInformation(nil)
	assert.Error(t, err)

}

func TestDownloadInformationRemoteSuccess(t *testing.T) {

	r, _ := NewContainerConverter("hello-world", "", nil)

	err := r.downloadImageInformation(&registryConfig{
		url: "https://registry-1.docker.io",
	})
	assert.NoError(t, err)

	// image config should have some values
	assert.NotNil(t, r.imageConfig.Cmd)

}
