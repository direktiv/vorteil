package vconvert

import (
	"fmt"
	"os"
	"strings"

	log "github.com/sirupsen/logrus"
)

const (
	localIdentifier = "local."
)

// ConvertContainer converts an application from local or remote
// container registries into vorteil virtual machines
func ConvertContainer(app, dest, user, pwd, config string) error {

	handler, err := newHandler(app, user, pwd, dest)
	if err != nil {
		return err
	}
	defer os.RemoveAll(handler.tmpDir)

	if strings.HasPrefix(handler.imageRef.Registry(), localIdentifier) {
		fmt.Printf("LOCAL >> %v\n", handler.imageRef.Registry())
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

	log.Infof("image %s created in %s", handler.imageRef.ShortName(), dest)

	return nil
}
