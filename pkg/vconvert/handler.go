package vconvert

// v1 "github.com/google/go-containerregistry/pkg/v1"
import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"sync"

	cmanifest "github.com/containers/image/manifest"
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema2"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/gosuri/uiprogress"
	"github.com/heroku/docker-registry-client/registry"
	parser "github.com/novln/docker-parser"
	"github.com/vorteil/vorteil/pkg/vcfg"
)

const (
	configRepo    = "repositories"
	configURL     = "url"
	workers       = 5
	tarExpression = "%s/%s.tar"
)

var (
	inbox = make(chan job)
	wg    sync.WaitGroup
)

type job struct {
	layer  interface{}
	bar    *uiprogress.Bar
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

func (ih *imageHandler) createVMFromRemote() error {

	repos, err := fetchURL(ih.imageRef.Registry())
	if err != nil {
		return err
	}

	url := repos[configURL]
	logFn("registry url: %s", url)

	hub, err := registry.New(url.(string), ih.user, ih.pwd)
	if err != nil {
		return err
	}

	ih.registry = hub

	manifest, err := hub.ManifestV2(ih.imageRef.ShortName(), ih.imageRef.Tag())
	if err != nil {
		return err
	}

	err = ih.downloadManifest(manifest.Manifest)
	if err != nil {
		logFn("can not download manifest: %s", err.Error())
	}

	ih.downloadBlobs(manifest.Manifest)

	return nil

}

func (ih *imageHandler) downloadManifest(manifest schema2.Manifest) error {

	logFn("downloading manifest")
	reader, err := ih.registry.DownloadBlob(ih.imageRef.ShortName(), manifest.Target().Digest)
	defer reader.Close()
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	n, err := buf.ReadFrom(reader)
	if err != nil {
		logFn("can not read from stream: %s", err.Error())
		return err
	}

	var img cmanifest.Schema2V1Image
	err = json.Unmarshal(buf.Bytes()[:n], &img)
	if err != nil {
		logFn("can not unmarshal manifest blob: %s\n%s", err.Error(), buf.String())
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

	logFn("downloading to %s\n", ih.tmpDir)
	logFn("downloading %d layer(s) for %s", len(manifest.Layers), ih.imageRef.ShortName())
	log.SetOutput(ioutil.Discard)

	var ifs = make([]interface{}, len(manifest.Layers))
	for i, d := range manifest.Layers {
		ifs[i] = d
	}
	ih.layers = ifs

	wg.Add(workers)
	go foreman(ifs)
	for i := 0; i < workers; i++ {
		go ih.worker(ih.tmpDir)
	}

	wg.Wait()
	uiprogress.Stop()
	log.SetOutput(os.Stdout)

}

func (ih *imageHandler) worker(dir string) {

	for {
		job, opened := <-inbox
		if !opened {
			break
		}

		layer := job.layer.(distribution.Descriptor)
		reader, err := ih.registry.DownloadBlob(ih.imageRef.ShortName(), layer.Digest)
		if err != nil {
			log.SetOutput(os.Stdout)
			log.Fatalf("can not download layer %s: %s", layer.Digest, err.Error())
			os.Exit(1)
		}
		defer reader.Close()

		if err != nil {
			log.SetOutput(os.Stdout)
			log.Fatalf("can not download layer %s: %s", layer.Digest, err.Error())
			os.Exit(1)
		}
		writeFile(fmt.Sprintf(tarExpression, dir, layer.Digest[7:15]), reader, job.bar, float64(layer.Size))
	}
	wg.Done()
}

func (ih *imageHandler) untarLayers(targetDir string) error {

	var filename string
	for _, l := range ih.layers {
		switch l.(type) {
		case v1.Layer:
			d, err := l.(v1.Layer).Digest()
			if err != nil {
				return err
			}
			filename = fmt.Sprintf(tarExpression, ih.tmpDir, d.Hex[7:15])
			break
		default:
			filename = fmt.Sprintf(tarExpression, ih.tmpDir, l.(distribution.Descriptor).Digest[7:15])
		}

		untarLayer(filename, targetDir)

	}

	return nil
}

func (ih *imageHandler) createVCFG(targetDir string) error {

	vcfgFile := new(vcfg.VCFG)

	ds, _ := vcfg.ParseBytes("+256 MB")
	vcfgFile.VM.DiskSize = ds

	ram, _ := vcfg.ParseBytes("256 MB")
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

	logFn("vcfg file:\n%v\n", string(b))

	return nil

}
