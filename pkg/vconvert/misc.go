package vconvert

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/distribution"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/gosuri/uiprogress"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

const (
	configFileName = "vconvert.yaml"
)

var (
	defaultProjectFile = `ignore = [".vorteilproject"]
[[target]]
  name = "default"
  vcfgs = ["default.vcfg"]`

	// ignore folders
	folders = []string{"dev/", "/dev/", "proc/", "/proc/",
		"sys/", "/sys/", "boot/", "/boot/", "media/", "/media/",
		"mnt/", "/mnt/"}
)

// reads in config file
// uses defaults if not found
func initConfig(cfgFile string) {

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := homedir.Dir()
		if err != nil {
			goto loadDefaults
		}
		viper.AddConfigPath(home)
		viper.SetConfigName(configFileName)
	}

loadDefaults:
	if err := viper.ReadInConfig(); err == nil {
		logFn("using config file: %s", viper.ConfigFileUsed())
	} else {
		if err != nil {
			logFn("%s\n", err.Error())
		}
		logFn("using default repositories")
		viper.SetDefault("repositories",
			map[string]interface{}{
				"docker.io":         map[string]interface{}{"url": "https://registry-1.docker.io"},
				"mcr.microsoft.com": map[string]interface{}{"url": "https://mcr.microsoft.com"},
				"gcr.io":            map[string]interface{}{"url": "https://gcr.io"},
			})
	}
}

// write function to write out tar layers
func writeFile(name string, r io.Reader, b *uiprogress.Bar, total float64) error {

	buf := make([]byte, 32768)
	read := 0

	f, err := os.Create(name)
	if err != nil {
		return err
	}
	for {
		n, err := r.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		if b != nil {
			read += n
			b.Set(int((float64(read) / total) * 100))
		}

		if _, err := f.Write(buf[:n]); err != nil {
			return err
		}
	}

	return nil
}

func fetchURL(repo string) (map[string]interface{}, error) {

	// fetch url
	repositoriesMap := viper.Get(configRepo)
	if repositoriesMap == nil {
		return nil, fmt.Errorf("No repositories specified")
	}

	repositoryMap := repositoriesMap.(map[string]interface{})[repo]

	if repositoryMap == nil {
		return nil, fmt.Errorf("No repository with name %s specified", repo)
	}

	return repositoryMap.(map[string]interface{}), nil
}

func foreman(layers []interface{}) {

	uiprogress.Start()
	for i, layer := range layers {
		job := job{
			layer:  layer,
			bar:    uiprogress.AddBar(100).AppendCompleted(),
			number: i,
		}
		job.bar.PrependFunc(func(b *uiprogress.Bar) string {
			digest := ""
			switch job.layer.(type) {
			case v1.Layer:
				d, err := job.layer.(v1.Layer).Digest()
				if err == nil {
					digest = d.Hex[7:15]
				}
				return fmt.Sprintf("layer %d (%s): ", job.number, digest)
			default:
				digest = fmt.Sprintf("%s", job.layer.(distribution.Descriptor).Digest[7:15])
			}
			return fmt.Sprintf("layer %d (%s): ", job.number, digest)
		})
		inbox <- job
	}
	close(inbox)
}

func skipFolder(folder string) bool {

	for _, f := range folders {
		if strings.HasPrefix(folder, f) {
			return true
		}
	}

	return false
}

func removeDirContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}
	}
	return nil
}

func untarLayer(fileName, toDir string) error {

	log.Printf("untar %s\n", fileName)
	var tr *tar.Reader

	f, err := os.Open(fileName)
	if err != nil {
		return err
	}
	defer f.Close()

	// check if it is a .gz.tar or tar
	gzr, err := gzip.NewReader(f)
	if err != nil {
		f.Seek(0, 0)
		tr = tar.NewReader(f)
		goto tar
	} else {
		tr = tar.NewReader(gzr)
		defer gzr.Close()
	}

tar:
	for {
		header, err := tr.Next()

		switch {
		case err == io.EOF:
			return nil
		case err != nil:
			return err
		case header == nil:
			continue
		}

		target := filepath.Join(toDir, header.Name)

		if skipFolder(header.Name) {
			continue
		}

		if strings.Contains(header.Name, ".wh..wh..opq") {
			parent := strings.ReplaceAll(header.Name, ".wh..wh..opq", "")
			removeDirContents(parent)
			continue
		}

		if strings.Contains(header.Name, ".wh.") {
			file := strings.ReplaceAll(target, ".wh.", "")
			err := os.RemoveAll(file)
			if err != nil {
				log.Fatalf("can not process .wh.: %s", err.Error())
			}
			continue
		}

		switch header.Typeflag {
		case tar.TypeSymlink:
			var err error
			if strings.HasPrefix(header.Linkname, "/") {
				rel, errRel := filepath.Rel(filepath.Dir(target), filepath.Join(toDir, header.Linkname))
				if errRel != nil {
					return errRel
				}
				err = os.Symlink(rel, target)
			} else {
				err = os.Symlink(header.Linkname, target)
			}
			if err != nil && !os.IsExist(err) {
				log.Printf("can not create symlink %s -> %s, %s", header.Linkname, target, err.Error())
				continue
			}
			break
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}
			break
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(0755))
			f.Truncate(0)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				return err
			}
			f.Close()
			break
		case tar.TypeLink:
			err := os.Link(filepath.Join(toDir, header.Linkname), target)
			if err != nil && !os.IsExist(err) {
				log.Printf("can not create link %s: %s", header.Name, err.Error())
			}
			break
		default:
			log.Printf("unknown file type in tar: %s, type %v", header.Name, header.Typeflag)
		}
	}

}

func findBinary(name string, env []string, cwd string, targetDir string) (string, error) {

	if strings.HasPrefix(name, "./") {
		abs, err := filepath.Abs(name)
		if err != nil {
			log.Printf("can not get absolute path for %s: %s", name, err.Error())
			return name, nil
		}
		cwd, err := os.Getwd()
		if err != nil {
			log.Printf("can not get current working dir %s: %s", name, err.Error())
			return name, nil
		}
		rel, err := filepath.Rel(cwd, abs)
		if err != nil {
			log.Printf("can not get relative path for %s: %s", name, err.Error())
			return name, nil
		}
		name = rel
	}

	// absolute
	if strings.HasPrefix(name, "/") {
		return name, nil
	}

	for _, e := range env {
		elems := strings.SplitN(e, "=", 2)
		if elems[0] == "PATH" {
			elems = strings.Split(elems[1], ":")
			for _, p := range elems {
				path := filepath.Join(targetDir, p, strings.ReplaceAll(name, "\"", ""))
				if _, err := os.Stat(path); err == nil {
					return filepath.Join(p, strings.ReplaceAll(name, "\"", "")), nil
				}
			}
		}
	}

	path := filepath.Join(targetDir, cwd, strings.ReplaceAll(name, "\"", ""))
	if _, err := os.Stat(path); err == nil {
		return filepath.Join(cwd, strings.ReplaceAll(name, "\"", "")), nil
	}

	return "", fmt.Errorf("can not find binary %s", name)
}
