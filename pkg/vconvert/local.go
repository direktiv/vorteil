package vconvert

import (
	"context"
	"io"
	"os"

	"github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	log "github.com/sirupsen/logrus"
)

func (ih *imageHandler) downloadDockerTar(image, tag string) error {
	log.Infof("getting local docker image %s (%s)", image, tag)

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

	outFile, err := os.Create("/tmp/kkk")
	if err != nil {
		return err
	}
	defer outFile.Close()
	_, err = io.Copy(outFile, r)
	if err != nil {
		return err
	}

	img, err := tarball.ImageFromPath("/tmp/kkk", nil)
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
	ih.layers = ifs

	config, err := img.ConfigFile()
	if err != nil {
		return err
	}
	ih.imageConfig = config.Config

	ih.downloadBlobs()

	return nil

}
