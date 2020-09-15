package vconvert

// v1 "github.com/google/go-containerregistry/pkg/v1"
import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"

	"github.com/containerd/containerd/archive"
	"github.com/containerd/containerd/archive/compression"
	cmanifest "github.com/containers/image/manifest"
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema2"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/heroku/docker-registry-client/registry"
	parser "github.com/novln/docker-parser"
	log "github.com/sirupsen/logrus"
	"github.com/vbauerster/mpb"
)

const (
	configRepo    = "repositories"
	configURL     = "url"
	workers       = 5
	tarExpression = "%s/%s.tar"

	defaultDiskSize = "+256 MB"
	defaultRAMSize  = "256 MB"
)

var (
	jobs = make(chan job)
	wg   sync.WaitGroup
)

type job struct {
	layer  interface{}
	bar    *mpb.Bar
	number int
}

type imageHandler struct {
	imageRef    *parser.Reference
	tmpDir      string
	layers      []interface{}
	imageConfig v1.Config
	user, pwd   string
	registry    *registry.Registry
}

func newHandler(app, user, pwd, dest string) (*imageHandler, error) {

	// get the ref first
	ref, err := parser.Parse(app)
	if err != nil {
		return nil, err
	}

	tmp, err := prepDirectories(dest)
	if err != nil {
		return nil, err
	}

	return &imageHandler{
		imageRef: ref,
		tmpDir:   tmp,
		user:     user,
		pwd:      pwd,
	}, nil

}

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

func (ih *imageHandler) createVMFromRemote() error {

	var url string

	repos, err := fetchRepoConfig(ih.imageRef.Registry())
	if err != nil {
		return err
	}

	if repos[configURL] == nil {
		return err
	}

	url = repos[configURL].(string)
	log.Infof("connecting to registry url: %s", url)

	hub, err := newRegistry(url, ih.user, ih.pwd)
	if err != nil {
		return err
	}
	ih.registry = hub

	log.Infof("fetching manifest for %s", ih.imageRef.ShortName())
	manifest, err := hub.ManifestV2(ih.imageRef.ShortName(), ih.imageRef.Tag())
	if err != nil {
		return err
	}

	err = ih.downloadManifest(manifest.Manifest)
	if err != nil {
		return err
	}

	ih.downloadBlobs(manifest.Manifest)

	return nil

}

func (ih *imageHandler) downloadManifest(manifest schema2.Manifest) error {

	log.Infof("downloading manifest file")
	reader, err := ih.registry.DownloadBlob(ih.imageRef.ShortName(), manifest.Target().Digest)
	defer reader.Close()
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	n, err := buf.ReadFrom(reader)
	if err != nil {
		return err
	}

	var img cmanifest.Schema2V1Image
	err = json.Unmarshal(buf.Bytes()[:n], &img)
	if err != nil {
		return err
	}

	ih.imageConfig.Cmd = make([]string, len(img.Config.Cmd))
	ih.imageConfig.Entrypoint = make([]string, len(img.Config.Entrypoint))
	ih.imageConfig.Env = make([]string, len(img.Config.Env))
	ih.imageConfig.ExposedPorts = make(map[string]struct{})
	for k, v := range img.Config.ExposedPorts {
		ih.imageConfig.ExposedPorts[string(k)] = v
	}

	copy(ih.imageConfig.Cmd, img.Config.Cmd)
	copy(ih.imageConfig.Entrypoint, img.Config.Entrypoint)
	copy(ih.imageConfig.Env, img.Config.Env)
	ih.imageConfig.WorkingDir = img.Config.WorkingDir

	return nil
}

func (ih *imageHandler) downloadBlobs(manifest schema2.Manifest) {

	if !elog.IsJSON {
		log.SetOutput(ioutil.Discard)
	}

	var ifs = make([]interface{}, len(manifest.Layers))
	for i, d := range manifest.Layers {
		ifs[i] = d
	}
	ih.layers = ifs

	p := mpb.New(mpb.WithWaitGroup(&wg))
	wg.Add(len(ih.layers))

	go distributor(ifs, p)
	for i := 0; i < workers; i++ {
		go worker(ih.tmpDir, ih.imageRef.ShortName(), ih.registry)
	}

	p.Wait()

	if !elog.IsJSON {
		log.SetOutput(os.Stdout)
	}
}

func (ih *imageHandler) untarLayers(targetDir string) error {

	for _, layer := range ih.layers {

		filename := fmt.Sprintf(tarExpression, ih.tmpDir, layer.(distribution.Descriptor).Digest[7:15])

		log.Infof("untar layer %s into %s", filename, targetDir)

		fn, err := os.Open(filename)
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

			// check if Directory
			if hdr.Mode&040000 != 0 {
				// check if in our skip list
				for _, f := range folders {
					// check 3 different variations of the folder
					// /folder folder /folder/
					fmts := []string{"/%s", "/%s/", "%s"}
					for _, f1 := range fmts {
						if hdr.Name == fmt.Sprintf(f1, f) {
							log.Debugf("skipping directory %s", f)
							return false, nil
						}
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

	return nil
}

func (ih *imageHandler) createVCFG(targetDir string) error {

	vcfgFile := new(vcfg.VCFG)

	ds, _ := vcfg.ParseBytes(defaultDiskSize)
	vcfgFile.VM.DiskSize = ds

	ram, _ := vcfg.ParseBytes(defaultRAMSize)
	vcfgFile.VM.RAM = ram

	vcfgFile.Programs = make([]vcfg.Program, 1)
	vcfgFile.Networks = make([]vcfg.NetworkInterface, 1)

	var finalCmd []string

	if len(ih.imageConfig.Entrypoint) > 0 {
		ss := []string(ih.imageConfig.Entrypoint)
		finalCmd = append(finalCmd, ss...)
	}

	if len(ih.imageConfig.Cmd) > 0 {
		finalCmd = append(finalCmd, ih.imageConfig.Cmd...)
	}

	vcfgFile.Programs[0].Cwd = ih.imageConfig.WorkingDir

	if len(finalCmd) == 0 {
		return fmt.Errorf("can not generate command: %s", finalCmd)
	}

	bin, err := findBinary(finalCmd[0], ih.imageConfig.Env, vcfgFile.Programs[0].Cwd, targetDir)
	if err != nil {
		return err
	}
	vcfgFile.Programs[0].Binary = bin

	var args []string

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

	// argsString := strings.Join(args, " ")
	// space := regexp.MustCompile(`\s+`)
	// s := space.ReplaceAllString(argsString, " ")
	// vcfgFile.Programs[0].Args = vcfg.Args(s)

	// environment variables
	vcfgFile.Programs[0].Env = ih.imageConfig.Env

	var portTCP []string
	var portUDP []string
	for key := range ih.imageConfig.ExposedPorts {
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
	ioutil.WriteFile(fmt.Sprintf("%s/.vorteilproject", targetDir), []byte(defaultProjectFile), 0644)
	ioutil.WriteFile(fmt.Sprintf("%s/default.vcfg", targetDir), b, 0644)

	log.Debugf("vcfg file:\n%v\n", string(b))

	return nil

}
