/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package vconvert

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/images/archive"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/platforms"
	"github.com/docker/docker/client"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/heroku/docker-registry-client/registry"
	log "github.com/sirupsen/logrus"
)

const (
	containerdSock = "/run/containerd/containerd.sock"
)

func (ih *ContainerConverter) downloadContainerdTar(image, tag string) error {

	log.Infof("getting local containerd image %s (%s)", image, tag)

	if len(image) == 0 || len(tag) == 0 {
		return fmt.Errorf("image and tag value required")
	}

	ctx := namespaces.WithNamespace(context.Background(), "default")

	client, err := containerd.New(containerdSock)
	if err != nil {
		return err
	}
	defer client.Close()

	o, err := ioutil.TempFile("", "cimg")
	if err != nil {
		return err
	}
	defer os.Remove(o.Name())

	img := fmt.Sprintf("%s:%s", image, tag)

	err = client.Export(ctx, o, archive.WithPlatform(platforms.Default()), archive.WithImage(client.ImageService(), img))
	if err != nil {
		return err
	}

	return ih.localHandler(o.Name())

}

func (ih *ContainerConverter) downloadDockerTar(image, tag string) error {

	log.Infof("getting local docker image %s (%s)", image, tag)

	if len(image) == 0 || len(tag) == 0 {
		return fmt.Errorf("image and tag value required")
	}

	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return err
	}
	cli.NegotiateAPIVersion(ctx)

	r, err := cli.ImageSave(ctx, []string{image})
	if err != nil {
		return err
	}
	defer r.Close()

	o, err := ioutil.TempFile("", "cimg")
	if err != nil {
		return err
	}
	defer os.Remove(o.Name())

	_, err = io.Copy(o, r)
	if err != nil {
		return err
	}

	// return ih.localHandler(o.Name())
	return nil
}

func (ih *ContainerConverter) localHandler(path string) error {

	// img, err := tarball.ImageFromPath(path, nil)
	// if err != nil {
	// 	return err
	// }
	//
	// layers, err := img.Layers()
	// if err != nil {
	// 	return err
	// }
	//
	// var ifs = make([]*layer, len(layers))
	// for i, d := range layers {
	// 	s, err := d.Size()
	// 	if err != nil {
	// 		return err
	// 	}
	// 	digest, err := d.Digest()
	// 	if err != nil {
	// 		return err
	// 	}
	// 	ifs[i] = &layer{
	// 		layer: d,
	// 		size:  s,
	// 		hash:  digest.Hex[7:15],
	// 	}
	//
	// }
	// ih.layers = ifs
	//
	// config, err := img.ConfigFile()
	// if err != nil {
	// 	return err
	// }
	// ih.imageConfig = config.Config

	// ih.downloadBlobs()

	return nil
}

func localGetReader(image string, layer *layer, registry *registry.Registry) (io.ReadCloser, error) {
	l := layer.layer.(v1.Layer)
	reader, err := l.Compressed()
	if err != nil {
		return nil, err
	}
	return reader, nil
}
