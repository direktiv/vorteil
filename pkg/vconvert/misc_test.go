package vconvert

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

//

func TestConfig(t *testing.T) {

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

}
