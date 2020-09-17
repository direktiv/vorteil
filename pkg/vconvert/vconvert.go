/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package vconvert

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"github.com/vorteil/vorteil/pkg/vcfg"

	"github.com/containerd/containerd/archive"
	"github.com/containerd/containerd/archive/compression"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	log "github.com/sirupsen/logrus"
	"github.com/vorteil/vorteil/pkg/elog"

	"github.com/heroku/docker-registry-client/registry"
	parser "github.com/novln/docker-parser"
)

const (
	configRepo    = "repositories"
	configURL     = "url"
	workers       = 5
	tarExpression = "%s/%s.tar"

	defaultDiskSize = "+256 MB"
	defaultRAMSize  = "256 MB"

	dockerRuntime     = "docker"
	containerdRuntime = "containerd"

	localIdentifier = "local."
)

// RegistryType defines if it is a image repository server or a local
// container runtime instance
type RegistryType string

// RegistryType values where local is e.g. docker and remote docker hub
var (
	LocalRegistry  RegistryType = "local"
	RemoteRegistry RegistryType = "remote"
	NullRegistry   RegistryType = ""
)

type job struct {
	layer  *layer
	dir    string
	number int

	// return value
	name string
	err  error
}

// compat struct for layers from local runtimes and remote image repos
type layer struct {
	layer interface{}
	size  int64
	hash  string

	file string
}

// ContainerConverter is the base object. Create a client with NewContainerConverter.
type ContainerConverter struct {
	imageRef *parser.Reference

	imageConfig  v1.Config
	registry     *registry.Registry
	registryType RegistryType

	layers      []*layer
	fetchReader func(string, *layer, *registry.Registry) (io.ReadCloser, error)
	jobsCh      chan *job
	jobsDoneCh  chan *job

	logger elog.View
}

// NewContainerConverter returns a ContainerConverter for the given image
// It parses and validates the application name., Logger elog.View
func NewContainerConverter(app string, log elog.View) (*ContainerConverter, error) {

	if log == nil {
		log = &elog.CLI{}
	}

	// get the ref first
	ref, err := parser.Parse(app)
	if err != nil {
		return nil, err
	}

	cc := &ContainerConverter{
		imageRef: ref,
	}

	if strings.HasPrefix(ref.Registry(), localIdentifier) {
		// we only support docker and containerd
		// we can assume %s.%s here
		s := strings.SplitN(ref.Registry(), ".", 2)

		switch s[1] {
		case dockerRuntime:
			fallthrough
		case containerdRuntime:
			{
				cc.fetchReader = localGetReader
				cc.registryType = LocalRegistry
			}
		default:
			{
				return nil, fmt.Errorf("unknown local container runtime")
			}
		}

	} else {
		cc.registryType = RemoteRegistry
		cc.fetchReader = remoteGetReader
	}

	cc.logger = log
	cc.jobsCh = make(chan *job)
	cc.jobsDoneCh = make(chan *job, workers)

	return cc, nil

}

// RegistryType returns the type of registry: local, remote or none
func (cc *ContainerConverter) RegistryType() RegistryType {
	return cc.registryType
}

// RegistryName returns the name of the image registry
// Returns empty string for local registries
func (cc *ContainerConverter) RegistryName() string {

	registry := ""

	if cc.registryType == RemoteRegistry {
		return cc.imageRef.Registry()
	}

	return registry

}

// DownloadImageInformation downloads the information required to download the
// layers of an image. After downloading the layer information is stored in ContainerConverter.
// For remote registries the config needs to be provided with at least the url of the registry.
func (cc *ContainerConverter) downloadImageInformation(config *RegistryConfig) error {

	if cc.imageRef == nil {
		return fmt.Errorf("image reference missing")
	}

	if cc.registryType == LocalRegistry {

	} else {
		cc.downloadInformationRemote(config)
	}

	return nil
}

// func (ih *ContainerConverter) downloadRemoteTar() error {
//
// 	var url string
//
// 	repos, err := fetchRepoConfig(ih.ImageRef.Registry())
// 	if err != nil {
// 		return err
// 	}
//
// 	if repos[configURL] == nil {
// 		return err
// 	}
//
// 	url = repos[configURL].(string)
// 	log.Infof("connecting to registry url: %s", url)
//
// 	hub, err := newRegistry(url, ih.user, ih.pwd)
// 	if err != nil {
// 		return err
// 	}
// 	ih.registry = hub
//
// 	log.Infof("fetching manifest for %s", ih.ImageRef.ShortName())
// 	manifest, err := hub.ManifestV2(ih.ImageRef.ShortName(), ih.ImageRef.Tag())
// 	if err != nil {
// 		return err
// 	}
//
// 	err = ih.downloadManifest(manifest.Manifest)
// 	if err != nil {
// 		return err
// 	}
//
// 	var ifs = make([]*layer, len(manifest.Layers))
// 	for i, d := range manifest.Layers {
// 		ifs[i] = &layer{
// 			layer: d,
// 			hash:  string(d.Digest[7:15]),
// 			size:  d.Size,
// 		}
// 	}
// 	ih.layers = ifs
//
// 	ih.downloadBlobs()
//
// 	return nil
// 	//
// }

// func (cc *ContainerConverter) downloadManifest(manifest schema2.Manifest) error {
//
// 	log.Infof("downloading manifest file")
// 	reader, err := cc.registry.DownloadBlob(cc.ImageRef.ShortName(), manifest.Target().Digest)
// 	defer reader.Close()
// 	if err != nil {
// 		return err
// 	}
//
// 	buf := new(bytes.Buffer)
// 	n, err := buf.ReadFrom(reader)
// 	if err != nil {
// 		return err
// 	}
//
// 	var img cmanifest.Schema2V1Image
// 	err = json.Unmarshal(buf.Bytes()[:n], &img)
// 	if err != nil {
// 		return err
// 	}
//
// 	ih.imageConfig.Cmd = make([]string, len(img.Config.Cmd))
// 	ih.imageConfig.Entrypoint = make([]string, len(img.Config.Entrypoint))
// 	ih.imageConfig.Env = make([]string, len(img.Config.Env))
// 	ih.imageConfig.ExposedPorts = make(map[string]struct{})
// 	for k, v := range img.Config.ExposedPorts {
// 		ih.imageConfig.ExposedPorts[string(k)] = v
// 	}
//
// 	copy(ih.imageConfig.Cmd, img.Config.Cmd)
// 	copy(ih.imageConfig.Entrypoint, img.Config.Entrypoint)
// 	copy(ih.imageConfig.Env, img.Config.Env)
// 	ih.imageConfig.WorkingDir = img.Config.WorkingDir
//
// 	return nil
// }

func (cc *ContainerConverter) downloadBlobs(dir string) error {

	cc.logger.Printf("downloading blobs")

	if dir == "" {
		return fmt.Errorf("directory not provided for downloads")
	}

	// start workers
	for i := 0; i < workers; i++ {
		go cc.blobDownloadWorker()
	}

	cc.logger.Printf("downloading %d layers", len(cc.layers))

	for i, layer := range cc.layers {

		job := &job{
			layer:  layer,
			number: i,
			dir:    dir,
		}
		cc.jobsCh <- job

	}

	r := 0
	for {
		j := <-cc.jobsDoneCh
		if j.err != nil {
			cc.logger.Errorf("error downloading layer: %s", j.err.Error())
		}
		r++

		// we have received all responses
		if r == len(cc.layers) {
			break
		}
	}

	cc.logger.Printf("download done")
	close(cc.jobsCh)

	return nil
}

func (cc *ContainerConverter) untarLayers(targetDir string) error {

	err := checkDirectoriy(targetDir)
	if err != nil {
		return err
	}

	for _, layer := range cc.layers {

		if layer.file == "" {
			return fmt.Errorf("no file associated with layer %s", layer.hash)
		}

		cc.logger.Printf("untar layer %s into %s", layer.file, targetDir)

		fn, err := os.Open(layer.file)
		if err != nil {
			return err
		}

		r, err := compression.DecompressStream(fn)
		if err != nil {
			return err
		}

		if _, err := archive.Apply(context.TODO(), targetDir, r, archive.WithFilter(func(hdr *tar.Header) (bool, error) {

			// we set everything to 1000, not important on windows
			hdr.Uid = 1000
			hdr.Gid = 1000

			// check if in our skip list
			for _, f := range folders {
				// check 3 different variations of the folder
				// /folder folder /folder/
				fmts := []string{"/%s", "/%s/", "%s"}
				for _, f1 := range fmts {
					if strings.HasPrefix(hdr.Name, fmt.Sprintf(f1, f)) {
						log.Debugf("skipping file/dir %s", hdr.Name)
						return false, nil
					}
				}
			}
			return true, nil

		})); err != nil {
			r.Close()
			return err
		}
		r.Close()
	}

	cc.logger.Printf("files created into %s", targetDir)

	return nil
}

func (cc *ContainerConverter) createVCFG(config v1.Config, targetDir string) error {

	if _, err := os.Stat(targetDir); err != nil {
		return fmt.Errorf("directory %s does not exist", targetDir)
	}

	vcfgFile := new(vcfg.VCFG)

	ds, _ := vcfg.ParseBytes(defaultDiskSize)
	vcfgFile.VM.DiskSize = ds

	ram, _ := vcfg.ParseBytes(defaultRAMSize)
	vcfgFile.VM.RAM = ram

	vcfgFile.Programs = make([]vcfg.Program, 1)
	vcfgFile.Networks = make([]vcfg.NetworkInterface, 1)

	var finalCmd []string

	if len(config.Entrypoint) > 0 {
		ss := []string(config.Entrypoint)
		finalCmd = append(finalCmd, ss...)
	}

	if len(config.Cmd) > 0 {
		finalCmd = append(finalCmd, config.Cmd...)
	}

	vcfgFile.Programs[0].Cwd = config.WorkingDir

	if len(finalCmd) == 0 {
		return fmt.Errorf("can not generate command: %s", finalCmd)
	}

	// bin, err := findBinary(finalCmd[0], ih.imageConfig.Env, vcfgFile.Programs[0].Cwd, targetDir)
	// if err != nil {
	// 	return err
	// }
	// vcfgFile.Programs[0].Binary = bin
	//
	// var args []string
	//
	// for _, arg := range finalCmd[1:] {
	// 	if len(arg) == 1 {
	// 		continue
	// 	}
	// 	if strings.Index(arg, " ") > 0 {
	// 		args = append(args, fmt.Sprintf("'%s'", arg))
	// 	} else {
	// 		args = append(args, arg)
	// 	}
	// }
	//
	// // argsString := strings.Join(args, " ")
	// // space := regexp.MustCompile(`\s+`)
	// // s := space.ReplaceAllString(argsString, " ")
	// // vcfgFile.Programs[0].Args = vcfg.Args(s)
	//
	// // environment variables
	// vcfgFile.Programs[0].Env = ih.imageConfig.Env
	//
	// var portTCP []string
	// var portUDP []string
	// for key := range ih.imageConfig.ExposedPorts {
	// 	p := strings.SplitN(string(key), "/", 2)
	// 	if len(p) == 2 {
	// 		if p[1] == "tcp" {
	// 			portTCP = append(portTCP, p[0])
	// 		} else {
	// 			portUDP = append(portUDP, p[0])
	// 		}
	// 	} else {
	// 		portTCP = append(portTCP, p[0])
	// 	}
	// }
	// vcfgFile.Networks[0].TCP = portTCP
	// vcfgFile.Networks[0].UDP = portUDP
	//
	b, err := vcfgFile.Marshal()
	if err != nil {
		return err
	}

	// write default.vcfg and .projectfile
	err = ioutil.WriteFile(fmt.Sprintf("%s/.vorteilproject", targetDir), []byte(defaultProjectFile), 0644)
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(fmt.Sprintf("%s/default.vcfg", targetDir), b, 0644)
	if err != nil {
		return err
	}

	cc.logger.Printf("vcfg file:\n%v\n", string(b))

	return nil

}

func (cc *ContainerConverter) blobDownloadWorker() {

	var (
		// filename string
		err    error
		reader io.ReadCloser
	)

	for {

		job, opened := <-cc.jobsCh
		if !opened {
			break
		}

		reader, err = cc.fetchReader(cc.imageRef.ShortName(), job.layer, cc.registry)
		if err != nil {
			goto cont
		}
		// cc.logger.Printf("THISIS2")
		// pr := cc.logger.NewProgress("wehat", "%", 0).ProxyReader(reader)

		//
		// 	// if we use json we don't show bars
		// 	var proxyReader io.ReadCloser
		// 	if !elog.IsJSON {
		// 		proxyReader = job.bar.ProxyReader(reader)
		// 	} else {
		// 		log.Infof("downloading layer %s (%s)", ih.ImageRef.ShortName(), bytefmt.ByteSize((uint64)(job.layer.size)))
		// 		proxyReader = reader
		// 	}
		// defer proxyReader.Close()
		defer reader.Close()

		job.name = fmt.Sprintf(tarExpression, job.dir, job.layer.hash)

		cc.logger.Printf("downloading file to %s", job.name)

		err = writeFile(job.name, reader)
		if err != nil {
			goto cont
		}

	cont:
		job.err = err

		// set the file to the layer
		cc.layers[job.number].file = job.name

		cc.jobsDoneCh <- job

	}

}
