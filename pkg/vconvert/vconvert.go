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

	log.Infof("HERE %s %s", handler.imageRef.Registry(), localIdentifier)

	if strings.HasPrefix(handler.imageRef.Registry(), localIdentifier) {
		// we only support docker and containerd
		// we can assume %s.%s here
		s := strings.SplitN(handler.imageRef.Registry(), ".", 2)
		switch s[1] {
		case "docker":
			{
				handler.fetchReader = dockerGetReader
				err := handler.downloadDockerTar(handler.imageRef.ShortName(), handler.imageRef.Tag())
				if err != nil {
					return err
				}
			}
		case "containerd":
			{
				log.Infof("CONTAINER!!!")
			}
		default:
			{
				return fmt.Errorf("unknown local container runtime")
			}
		}

	} else {
		initConfig(config)
		handler.fetchReader = remoteGetReader
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
