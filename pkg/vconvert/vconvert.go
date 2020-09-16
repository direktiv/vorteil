/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package vconvert

import (
	"os"

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

	initConfig(config)

	err = handler.createTar()
	if err != nil {
		return err
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
