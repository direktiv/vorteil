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
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"

	"github.com/containerd/containerd/archive"
	"github.com/containerd/containerd/archive/compression"
	v1 "github.com/google/go-containerregistry/pkg/v1"

	clog "github.com/containerd/containerd/log"
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
	DockerRegistry     RegistryType = "docker"
	ContainerdRegistry RegistryType = "containerd"
	RemoteRegistry     RegistryType = "remote"
	NullRegistry       RegistryType = ""
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
	tmpLocalTar *os.File

	logger elog.View
}

// NewContainerConverter returns a ContainerConverter for the given image
// It parses and validates the application name., Logger elog.View
func NewContainerConverter(app, config string, log elog.View) (*ContainerConverter, error) {

	if log == nil {
		log = &elog.CLI{}
	}

	initConfig(config, log)

	// get the ref first
	ref, err := parser.Parse(app)
	if err != nil {
		return nil, err
	}

	log.Printf("convert image: %s", ref.Name())

	cc := &ContainerConverter{
		imageRef: ref,
	}

	if strings.HasPrefix(ref.Registry(), localIdentifier) {
		// we only support docker and containerd
		// we can assume %s.%s here
		s := strings.SplitN(ref.Registry(), ".", 2)

		switch s[1] {
		case dockerRuntime:
			cc.fetchReader = localGetReader
			cc.registryType = DockerRegistry
		case containerdRuntime:
			cc.fetchReader = localGetReader
			cc.registryType = ContainerdRegistry
		default:
			return nil, fmt.Errorf("unknown local container runtime")
		}

	} else {
		cc.registryType = RemoteRegistry
		cc.fetchReader = remoteGetReader
	}

	cc.logger = log
	cc.jobsCh = make(chan *job, workers)
	cc.jobsDoneCh = make(chan *job, workers)

	return cc, nil

}

// ConvertToProject exports a container image as a vorteil.io VM into the dst directory
func (cc *ContainerConverter) ConvertToProject(dst, user, pwd string) error {

	// check if folder exists
	err := checkDirectory(dst)
	if err != nil {
		return err
	}

	var (
		url string
	)

	if cc.RegistryType() == RemoteRegistry {
		reg, err := fetchRepoConfig(cc.RegistryName())
		if err != nil {
			return err
		}

		url = reg["url"].(string)
		if url == "" {
			return fmt.Errorf("url not available for registry %s", cc.RegistryName())
		}

		cc.logger.Printf("registry %s, url %s", cc.RegistryName(), url)

	} else {
		cc.logger.Printf("registry %s", cc.RegistryType())
	}

	err = cc.downloadImageInformation(&registryConfig{
		url:  url,
		user: user,
		pwd:  pwd,
	})

	if err != nil {
		return err
	}

	dir, _ := ioutil.TempDir("", "vtest")
	defer os.RemoveAll(dir)

	err = cc.downloadBlobs(dir)
	if err != nil {
		return err
	}

	err = cc.untarLayers(dst)
	if err != nil {
		return err
	}

	err = cc.createVCFG(cc.imageConfig, dst)
	if err != nil {
		return err
	}

	return nil
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

// downloadImageInformation downloads the information required to download the
// layers of an image. After downloading the layer information is stored in ContainerConverter.
// For remote registries the config needs to be provided with at least the url of the registry.
func (cc *ContainerConverter) downloadImageInformation(config *registryConfig) error {

	if cc.imageRef == nil {
		return fmt.Errorf("image reference missing")
	}

	var err error

	switch cc.registryType {
	case DockerRegistry:
		err = cc.downloadInformationDocker(cc.imageRef.ShortName(), cc.imageRef.Tag())
	case ContainerdRegistry:
		err = cc.downloadInformationContainerd(cc.imageRef.ShortName(), cc.imageRef.Tag())
	default:
		err = cc.downloadInformationRemote(config)
	}

	return err
}

func (cc *ContainerConverter) downloadBlobs(dir string) error {

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

	cc.logger.Debugf("all %d jobs sent", len(cc.layers))
	r := 0
	for {
		j := <-cc.jobsDoneCh
		if j.err != nil {
			cc.logger.Errorf("error downloading layer: %s", j.err.Error())
		}
		r++

		cc.logger.Debugf("received %d from %d jobs finished", r, len(cc.layers))

		// we have received all responses
		if r == len(cc.layers) {
			break
		}
	}

	cc.logger.Printf("all layers downloaded")
	close(cc.jobsCh)

	if cc.tmpLocalTar != nil {
		os.Remove(cc.tmpLocalTar.Name())
		cc.tmpLocalTar = nil
	}

	return nil
}

func (cc *ContainerConverter) untarLayers(targetDir string) error {

	err := checkDirectory(targetDir)
	if err != nil {
		return err
	}

	for _, layer := range cc.layers {

		if layer.file == "" {
			return fmt.Errorf("no file associated with layer %s", layer.hash)
		}

		cc.logger.Printf("unpack layer %s into %s", layer.file, targetDir)

		fn, err := os.Open(layer.file)
		if err != nil {
			return err
		}

		// suppressing containerd logs
		clog.L.Logger.SetLevel(logrus.ErrorLevel)

		r, err := compression.DecompressStream(fn)
		if err != nil {
			return err
		}

		if _, err := archive.Apply(context.TODO(), targetDir, r, archive.WithFilter(func(hdr *tar.Header) (bool, error) {

			// fetching uid, gid
			hdr.Uid, hdr.Gid = fetchUIDandGID()

			// check if in our skip list
			for _, f := range folders {
				// check 3 different variations of the folder
				// /folder folder /folder/
				fmts := []string{"/%s", "/%s/", "%s"}
				for _, f1 := range fmts {
					if strings.HasPrefix(hdr.Name, fmt.Sprintf(f1, f)) {
						cc.logger.Debugf("skipping file/dir %s", hdr.Name)
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

	bin, err := findBinary(finalCmd[0], config.Env, config.WorkingDir, targetDir, cc.logger)
	if err != nil {
		return err
	}

	var args []string
	args = append(args, bin)

	for _, arg := range finalCmd[1:] {
		if len(arg) == 1 {
			continue
		}
		if strings.Index(arg, " ") > 0 {
			args = append(args, fmt.Sprintf("'%s'", arg))
		} else {
			args = append(args, arg)
		}
	}

	argsString := strings.Join(args, " ")
	space := regexp.MustCompile(`\s+`)
	s := space.ReplaceAllString(argsString, " ")
	vcfgFile.Programs[0].Args = s

	// environment variables
	vcfgFile.Programs[0].Env = config.Env

	var portTCP []string
	var portUDP []string
	for key := range config.ExposedPorts {
		p := strings.SplitN(string(key), "/", 2)
		if len(p) == 2 {
			if p[1] == "tcp" {
				portTCP = append(portTCP, p[0])
			} else {
				portUDP = append(portUDP, p[0])
			}
		} else {
			portTCP = append(portTCP, p[0])
		}
	}
	vcfgFile.Networks[0].TCP = portTCP
	vcfgFile.Networks[0].UDP = portUDP

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

	cc.logger.Debugf("vcfg file:\n%v\n", string(b))

	return nil

}

func (cc *ContainerConverter) blobDownloadWorker() {

	var (
		err    error
		reader io.ReadCloser
		pr     io.ReadCloser
		p      elog.Progress
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

		p = cc.logger.NewProgress(fmt.Sprintf("layer %s:", job.layer.hash), "KiB", job.layer.size)
		pr = p.ProxyReader(reader)

		job.name = fmt.Sprintf(tarExpression, job.dir, job.layer.hash)

		err = writeFile(job.name, pr)
		if err != nil {
			goto cont
		}

	cont:
		job.err = err

		if err != nil {
			p.Finish(false)
		} else {
			p.Finish(true)
		}

		pr.Close()

		// set the file to the layer
		cc.layers[job.number].file = job.name
		cc.jobsDoneCh <- job

	}

}
