/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package vconvert

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/docker/distribution"
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

	var ifs = make([]*layer, len(manifest.Layers))
	for i, d := range manifest.Layers {
		ifs[i] = &layer{
			layer: d,
			hash:  string(d.Digest[7:15]),
			size:  d.Size,
		}
	}
	cc.layers = ifs

	// cc.downloadBlobs()

	// blob, err = cc.downloadManifest(manifest.Manifest)
	// if err != nil {
	// 	return err
	// }

	// var url string
	//
	// repos, err := fetchRepoConfig(ih.ImageRef.Registry())
	// if err != nil {
	// 	return err
	// }
	//
	// if repos[configURL] == nil {
	// 	return err
	// }
	//
	// url = repos[configURL].(string)
	// log.Infof("connecting to registry url: %s", url)
	//
	// hub, err := newRegistry(url, ih.user, ih.pwd)
	// if err != nil {
	// 	return err
	// }
	// ih.registry = hub
	//
	// log.Infof("fetching manifest for %s", ih.ImageRef.ShortName())
	// manifest, err := hub.ManifestV2(ih.ImageRef.ShortName(), ih.ImageRef.Tag())
	// if err != nil {
	// 	return err
	// }
	//
	// err = ih.downloadManifest(manifest.Manifest)
	// if err != nil {
	// 	return err
	// }
	//
	// var ifs = make([]*layer, len(manifest.Layers))
	// for i, d := range manifest.Layers {
	// 	ifs[i] = &layer{
	// 		layer: d,
	// 		hash:  string(d.Digest[7:15]),
	// 		size:  d.Size,
	// 	}
	// }
	// ih.layers = ifs
	//
	// ih.downloadBlobs()
	//
	return nil

}

// func (cc *ContainerConverter) downloadManifest(manifest schema2.Manifest) (*cmanifest.Schema2V1Image, error) {
//
// 	// log.Infof("downloading manifest file")
// 	reader, err := cc.registry.DownloadBlob(cc.ImageRef.ShortName(), manifest.Target().Digest)
// 	defer reader.Close()
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	buf := new(bytes.Buffer)
// 	n, err := buf.ReadFrom(reader)
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	img := &cmanifest.Schema2V1Image{}
// 	err = json.Unmarshal(buf.Bytes()[:n], img)
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	// ih.imageConfig.Cmd = make([]string, len(img.Config.Cmd))
// 	// ih.imageConfig.Entrypoint = make([]string, len(img.Config.Entrypoint))
// 	// ih.imageConfig.Env = make([]string, len(img.Config.Env))
// 	// ih.imageConfig.ExposedPorts = make(map[string]struct{})
// 	// for k, v := range img.Config.ExposedPorts {
// 	// 	ih.imageConfig.ExposedPorts[string(k)] = v
// 	// }
// 	//
// 	// copy(ih.imageConfig.Cmd, img.Config.Cmd)
// 	// copy(ih.imageConfig.Entrypoint, img.Config.Entrypoint)
// 	// copy(ih.imageConfig.Env, img.Config.Env)
// 	// ih.imageConfig.WorkingDir = img.Config.WorkingDir
//
// 	return img, nil
// }
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
