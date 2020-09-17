package vconvert

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestConfig(t *testing.T) {

	viper.Reset()
	initConfig("../../test/vconvert/config.yml")

	v, err := fetchRepoConfig("value1")
	assert.NoError(t, err)
	assert.NotNil(t, v)
	assert.NotNil(t, v["url"])
	assert.Equal(t, v["url"], "https://myurl")

}

func TestConfigNotExist(t *testing.T) {

	viper.Reset()
	initConfig("/does/no/exist")

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
	initConfig("")

	testURL("docker.io")
	testURL("mcr.microsoft.com")
	testURL("gcr.io")

}
