package vconvert

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	parser "github.com/novln/docker-parser"
)

const (
	localIdentifier = "local."
)

func logFn(format string, args ...interface{}) {
	log.Printf(format, args...)
}

// ConvertContainer converts an application from local or remote
// container registries into vorteil virtual machines
func ConvertContainer(app, dest, user, pwd, config string) error {

	ref, err := parser.Parse(app)
	if err != nil {
		return err
	}

	tmp, err := prepDirectories(dest)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	handler := &imageHandler{
		imageRef: ref,
		tmpDir:   tmp,
		user:     user,
		pwd:      pwd,
	}

	if strings.HasPrefix(ref.Registry(), localIdentifier) {
		fmt.Printf("LOCAL >> %v\n", ref.Registry())
	} else {
		initConfig(config)
		err = handler.createVMFromRemote()
		if err != nil {
			return err
		}
	}

	err = handler.untarLayers(dest)
	if err != nil {
		return err
	}

	err = handler.createVCFG(dest)
	if err != nil {
		return err
	}

	return nil
}

func prepDirectories(targetDir string) (string, error) {

	// check if it exists and empty
	if _, err := os.Stat(targetDir); err != nil {
		os.MkdirAll(targetDir, 0755)
	}

	fi, err := ioutil.ReadDir(targetDir)
	if err != nil {
		return "", err
	}
	if len(fi) > 0 {
		return "", fmt.Errorf("target directory not empty")
	}

	// create temporary extract folder
	dir, err := ioutil.TempDir(os.TempDir(), "image")
	if err != nil {
		return "", err
	}

	return dir, nil

}
