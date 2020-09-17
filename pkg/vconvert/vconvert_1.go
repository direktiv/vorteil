/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package vconvert

const (
	localIdentifier = "local."
)

// ConvertContainer converts an application from local or remote
// container registries into vorteil virtual machines
func ConvertContainer(app, dest, user, pwd, config string) error {

	//
	// if err != nil {
	// 	return err
	// }
	// defer os.RemoveAll(handler.tmpDir)
	//
	initConfig(config)
	// converter, err := NewContainerConverter(app, dest)
	// if err != nil {
	//
	// }
	// log.Infof("REGISTRY %s", converter.ImageRef.Registry())

	//
	// err = handler.DownloadTar("/h")
	// if err != nil {
	// 	return err
	// }
	//
	// err = handler.untarLayers(dest)
	// if err != nil {
	// 	return err
	// }
	//
	// err = handler.createVCFG(dest)
	// if err != nil {
	// 	return err
	// }
	//
	// log.Infof("image %s created in %s", handler.imageRef.ShortName(), dest)

	return nil
}
