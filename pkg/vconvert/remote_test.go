package vconvert

// NOTE: These tests are likely to fail on Travis CI/CD due to dockers request rate limiting.

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

// func TestDownloadInformationRemoteFailure(t *testing.T) {

// 	r := &ContainerConverter{}

// 	// should all fail
// 	var cc = []struct {
// 		config *registryConfig
// 	}{
// 		{nil},
// 		{&registryConfig{}},
// 	}

// 	for _, c := range cc {
// 		err := r.downloadInformationRemote(c.config)
// 		assert.Error(t, err)
// 	}

// 	err := r.downloadImageInformation(nil)
// 	assert.Error(t, err)

// }

// func TestDownloadInformationRemoteSuccess(t *testing.T) {

// 	r, _ := NewContainerConverter("hello-world", "", nil)

// 	err := r.downloadImageInformation(&registryConfig{
// 		url: "https://registry-1.docker.io",
// 	})
// 	assert.NoError(t, err)

// 	// image config should have some values
// 	assert.NotNil(t, r.imageConfig.Cmd)

// }
