package vconvert

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/images/archive"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/platforms"
	"github.com/docker/docker/client"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/heroku/docker-registry-client/registry"
)

const (
	containerdSock = "/run/containerd/containerd.sock"
)

func (cc *ContainerConverter) downloadInformationContainerd(image, tag string) error {

	cc.logger.Infof("getting local containerd image %s (%s)", image, tag)

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

	cc.tmpLocalTar = o

	img := fmt.Sprintf("%s:%s", image, tag)

	err = client.Export(ctx, cc.tmpLocalTar, archive.WithPlatform(platforms.Default()), archive.WithImage(client.ImageService(), img))
	if err != nil {
		return err
	}

	return cc.localHandler(cc.tmpLocalTar.Name())

}

func (cc *ContainerConverter) downloadInformationDocker(image, tag string) error {

	if len(image) == 0 || len(tag) == 0 {
		return fmt.Errorf("image and tag value required")
	}

	cc.logger.Printf("getting local docker image %s (%s)", image, tag)

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
	cc.tmpLocalTar = o

	_, err = io.Copy(cc.tmpLocalTar, r)
	if err != nil {
		return err
	}

	return cc.localHandler(cc.tmpLocalTar.Name())

}

func (cc *ContainerConverter) localHandler(path string) error {

	img, err := tarball.ImageFromPath(path, nil)
	if err != nil {
		return err
	}

	layers, err := img.Layers()
	if err != nil {
		return err
	}

	var ifs = make([]*layer, len(layers))
	for i, d := range layers {
		s, err := d.Size()
		if err != nil {
			return err
		}
		digest, err := d.Digest()
		if err != nil {
			return err
		}
		ifs[i] = &layer{
			layer: d,
			size:  s,
			hash:  digest.Hex[7:15],
		}

	}
	cc.layers = ifs

	config, err := img.ConfigFile()
	if err != nil {
		return err
	}
	cc.imageConfig = config.Config

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
