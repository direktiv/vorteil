package vconvert

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoteHighLevelConvert(t *testing.T) {

	f, _ := ioutil.TempDir("", "vtest")
	defer os.Remove(f)
	err := ConvertContainer("hello-world", f, "", "", "")
	assert.NoError(t, err)

	files, _ := ioutil.ReadDir(f)
	assert.Equal(t, 3, len(files))

}
