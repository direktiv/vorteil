package vconvert

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/vorteil/vorteil/pkg/elog"
)

func TestConfig(t *testing.T) {

	log := &elog.CLI{}

	viper.Reset()
	initConfig("../../test/vconvert/config.yml", log)

	v, err := fetchRepoConfig("value1")
	assert.NoError(t, err)
	assert.NotNil(t, v)
	assert.NotNil(t, v["url"])
	assert.Equal(t, v["url"], "https://myurl")

}

func TestConfigNotExist(t *testing.T) {

	log := &elog.CLI{}

	viper.Reset()
	initConfig("/does/no/exist", log)

	testURL := func(name string) {
		v, err := fetchRepoConfig(name)
		assert.NoError(t, err)
		assert.NotNil(t, v)
		assert.NotNil(t, v["url"])
	}

	testURL("docker.io")
	testURL("mcr.microsoft.com")
	testURL("gcr.io")

	// go to home dir and file does not exist
	viper.Reset()
	initConfig("", log)

	testURL("docker.io")
	testURL("mcr.microsoft.com")
	testURL("gcr.io")

}
