package vconvert

// import (
// 	"io/ioutil"
// 	"os"
// 	"strings"
// 	"testing"
//
// 	"github.com/spf13/viper"
// 	"github.com/stretchr/testify/assert"
// )
//
// func TestConfigNotInit(t *testing.T) {
//
// 	viper.Reset()
// 	_, err := fetchRepoConfig("value1")
// 	assert.Error(t, err)
//
// }
//
// func TestConfig(t *testing.T) {
//
// 	viper.Reset()
// 	initConfig("../../test/vconvert/config.yml")
//
// 	v, err := fetchRepoConfig("value1")
// 	assert.NoError(t, err)
// 	assert.NotNil(t, v)
// 	assert.NotNil(t, v["url"])
// 	assert.Equal(t, v["url"], "https://myurl")
//
// }
//
// func TestConfigNotExist(t *testing.T) {
//
// 	viper.Reset()
// 	initConfig("/does/no/exist")
//
// 	testURL := func(name string) {
// 		v, err := fetchRepoConfig(name)
// 		assert.NoError(t, err)
// 		assert.NotNil(t, v)
// 		assert.NotNil(t, v["url"])
// 	}
//
// 	testURL("docker.io")
// 	testURL("mcr.microsoft.com")
// 	testURL("gcr.io")
//
// 	// go to home dir and file does not exist
// 	viper.Reset()
// 	initConfig("")
//
// 	testURL("docker.io")
// 	testURL("mcr.microsoft.com")
// 	testURL("gcr.io")
//
// }
//
// func TestWriteFile(t *testing.T) {
//
// 	testString := "vorteil"
// 	r := strings.NewReader(testString)
// 	f, _ := ioutil.TempFile("", "vtest")
// 	defer os.Remove(f.Name())
//
// 	writeFile(f.Name(), r)
//
// 	fi, _ := f.Stat()
// 	assert.Equal(t, fi.Size(), (int64)(len(testString)))
//
// 	r = strings.NewReader(testString)
// 	c, _ := ioutil.ReadAll(r)
// 	assert.Equal(t, testString, string(c))
//
// }
//
// func TestFindBinary(t *testing.T) {
//
// 	s, err := findBinary("/find/bin", []string{}, "/", "../../test/vconvert")
// 	assert.NotNil(t, s)
//
// 	_, err = findBinary("does/not/exist", []string{}, "/", "../../test/vconvert")
// 	assert.Error(t, err)
//
// 	s, err = findBinary("bin", []string{}, "/find", "../../test/vconvert")
// 	assert.NotNil(t, s)
//
// 	_, err = findBinary("bin", []string{}, "/findDont", "../../test/vconvert")
// 	assert.Error(t, err)
//
// 	s, err = findBinary("./bin", []string{}, "/find", "../../test/vconvert")
// 	assert.NotNil(t, s)
//
// 	s, err = findBinary("/find/bin", []string{}, "/", "../../test/vconvert")
// 	assert.NoError(t, err)
//
// 	_, err = findBinary("/notfind/bin", []string{}, "/", "../../test/vconvert")
// 	assert.Error(t, err)
//
// 	s, err = findBinary("bin", []string{"PATH=/find"}, "/", "../../test/vconvert")
// 	assert.NotNil(t, s)
//
// }
//
// func TestPrepDirs(t *testing.T) {
//
// 	_, err := prepDirectories("../../test/vconvert")
// 	assert.Error(t, err)
//
// 	prepDir := "../../test/vconvert/prep"
// 	os.Remove(prepDir)
//
// 	_, err = prepDirectories(prepDir)
// 	assert.NoError(t, err)
//
// 	os.Remove(prepDir)
//
// }
