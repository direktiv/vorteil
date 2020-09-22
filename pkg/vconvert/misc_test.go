package vconvert

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestConfigNotInit(t *testing.T) {

	viper.Reset()
	_, err := fetchRepoConfig("value1")
	assert.Error(t, err)

}

func TestWriteFile(t *testing.T) {

	testString := "vorteil"
	r := strings.NewReader(testString)
	f, _ := ioutil.TempFile("", "vtest")
	defer os.Remove(f.Name())

	writeFile(f.Name(), r)

	fi, _ := f.Stat()
	assert.Equal(t, fi.Size(), (int64)(len(testString)))

	r = strings.NewReader(testString)
	c, _ := ioutil.ReadAll(r)
	assert.Equal(t, testString, string(c))

}

// func TestFindBinary(t *testing.T) {

// 	log := &elog.CLI{}

// 	s, _ := findBinary("/find/bin", []string{}, "/", "../../test/vconvert", log)
// 	assert.NotNil(t, s)

// 	_, err := findBinary("does/not/exist", []string{}, "/", "../../test/vconvert", log)
// 	assert.Error(t, err)

// 	s, _ = findBinary("bin", []string{}, "/find", "../../test/vconvert", log)
// 	assert.NotNil(t, s)

// 	_, err = findBinary("bin", []string{}, "/findDont", "../../test/vconvert", log)
// 	assert.Error(t, err)

// 	s, _ = findBinary("./bin", []string{}, "/find", "../../test/vconvert", log)
// 	assert.NotNil(t, s)

// 	_, err = findBinary("/find/bin", []string{}, "/", "../../test/vconvert", log)
// 	assert.NoError(t, err)

// 	_, err = findBinary("/notfind/bin", []string{}, "/", "../../test/vconvert", log)
// 	assert.Error(t, err)

// 	s, _ = findBinary("bin", []string{"PATH=/find"}, "/", "../../test/vconvert", log)
// 	assert.NotNil(t, s)

// }

// func TestPrepDirs(t *testing.T) {

// 	err := checkDirectory("../../test/vconvert")
// 	assert.Error(t, err)

// 	prepDir := "../../test/vconvert/prep"
// 	os.Remove(prepDir)

// 	err = checkDirectory(prepDir)
// 	assert.NoError(t, err)

// 	os.Remove(prepDir)

// }
