/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package vconvert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	cmanifest "github.com/containers/image/manifest"
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/heroku/docker-registry-client/registry"
	log "github.com/sirupsen/logrus"
)

// RegistryConfig contains the url of the remote registry.
type RegistryConfig struct {
	URL, User, Pwd string
}

func remoteGetReader(image string, layer *layer, registry *registry.Registry) (io.ReadCloser, error) {
	olayer := layer.layer.(distribution.Descriptor)
	reader, err := registry.DownloadBlob(image, olayer.Digest)
	if err != nil {
		return nil, err
	}
	return reader, nil
}

func (cc *ContainerConverter) downloadInformationRemote(config *RegistryConfig) error {

	if config == nil || config.URL == "" {
		return fmt.Errorf("config is nil or URL is empty")
	}

	r, err := newRegistry(config.URL, config.User, config.Pwd)
	if err != nil {
		return err
	}

	cc.registry = r

	manifest, err := r.ManifestV2(cc.imageRef.ShortName(), cc.imageRef.Tag())
	if err != nil {
		return err
	}

	_, err = cc.downloadManifest(manifest.Manifest)
	if err != nil {
		return err
	}

	var ifs = make([]*layer, len(manifest.Layers))
	for i, d := range manifest.Layers {
		ifs[i] = &layer{
			layer: d,
			hash:  string(d.Digest[7:15]),
			size:  d.Size,
		}
	}
	cc.layers = ifs

	return nil

}

func (cc *ContainerConverter) downloadManifest(manifest schema2.Manifest) (*cmanifest.Schema2V1Image, error) {

	cc.logger.Infof("downloading manifest file")

	reader, err := cc.registry.DownloadBlob(cc.imageRef.ShortName(), manifest.Target().Digest)
	defer reader.Close()
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	n, err := buf.ReadFrom(reader)
	if err != nil {
		return nil, err
	}

	img := &cmanifest.Schema2V1Image{}
	err = json.Unmarshal(buf.Bytes()[:n], img)
	if err != nil {
		return nil, err
	}

	cc.imageConfig.Cmd = make([]string, len(img.Config.Cmd))
	cc.imageConfig.Entrypoint = make([]string, len(img.Config.Entrypoint))
	cc.imageConfig.Env = make([]string, len(img.Config.Env))
	cc.imageConfig.ExposedPorts = make(map[string]struct{})
	for k, v := range img.Config.ExposedPorts {
		cc.imageConfig.ExposedPorts[string(k)] = v
	}

	copy(cc.imageConfig.Cmd, img.Config.Cmd)
	copy(cc.imageConfig.Entrypoint, img.Config.Entrypoint)
	copy(cc.imageConfig.Env, img.Config.Env)
	cc.imageConfig.WorkingDir = img.Config.WorkingDir

	return img, nil
}

//
// although there is a New(...) function in the registry
// but there is no way to set the log function before
// this function is basically a copy of the original New(...) function
func newRegistry(registryURL, user, pwd string) (*registry.Registry, error) {

	url := strings.TrimSuffix(registryURL, "/")
	transport := http.DefaultTransport
	transport = registry.WrapTransport(transport, url, user, pwd)
	registry := &registry.Registry{
		URL: url,
		Client: &http.Client{
			Transport: transport,
		},
		Logf: log.Debugf,
	}

	if err := registry.Ping(); err != nil {
		return nil, err
	}

	return registry, nil
}
