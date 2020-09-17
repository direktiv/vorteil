/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package vconvert

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	// "github.com/docker/distribution"

	"github.com/mitchellh/go-homedir"
	log "github.com/sirupsen/logrus"
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
	folders = []string{"dev", "proc", "sys", "boot", "media", "mnt"}
)

// reads in config file, uses defaults if not found
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
		log.Debugf("using config file: %s", viper.ConfigFileUsed())
	} else {
		if err != nil {
			log.Debugf("%s\n", err.Error())
		}
		log.Debugf("using default repositories")
		viper.SetDefault("repositories",
			map[string]interface{}{
				"docker.io":         map[string]interface{}{"url": "https://registry-1.docker.io"},
				"mcr.microsoft.com": map[string]interface{}{"url": "https://mcr.microsoft.com"},
				"gcr.io":            map[string]interface{}{"url": "https://gcr.io"},
			})
	}
}

// write function to write out tar layers
func writeFile(name string, r io.Reader) error {

	buf := make([]byte, 32768)

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

		if _, err := f.Write(buf[:n]); err != nil {
			return err
		}
	}

	return nil
}

func fetchRepoConfig(repo string) (map[string]interface{}, error) {

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

// findBinary tries to find the executable in the expanded container image
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
		fp := filepath.Join(targetDir, name)
		if _, err := os.Stat(fp); err == nil {
			return name, nil
		}
		return "", fmt.Errorf("can not find binary %s", name)
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

func checkDirectories(targetDir string) error {

	// check if it exists and empty
	if _, err := os.Stat(targetDir); err != nil {
		os.MkdirAll(targetDir, 0755)
	}

	fi, err := ioutil.ReadDir(targetDir)
	if err != nil {
		return err
	}
	if len(fi) > 0 {
		return fmt.Errorf("target directory %s not empty", targetDir)
	}

	// create temporary extract folder
	// dir, err := ioutil.TempDir(os.TempDir(), "image")
	// if err != nil {
	// 	return "", err
	// }

	return nil

}

// func distributor() {
//
// }

// func distributor(layers []*layer, p *mpb.Progress, jobs chan job) {
//
// 	log.Infof("downloading %d layers", len(layers))
// 	for i, layer := range layers {
//
// 		job := job{
// 			layer:  layer,
// 			number: i,
// 		}
//
// 		if !elog.IsJSON {
// 			job.bar = p.AddBar(int64(layer.size),
// 				mpb.PrependDecorators(
// 					decor.Name(fmt.Sprintf("layer %d (%s): ", i, layer.hash)),
// 				),
// 				mpb.AppendDecorators(
// 					decor.OnComplete(
// 						decor.CountersKiloByte("%.1f / %.1f"), "downloaded",
// 					),
// 				),
// 			)
// 		}
// 		jobs <- job
// 	}
//
// 	close(jobs)
// }
