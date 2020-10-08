package main

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/mitchellh/go-homedir"
	"github.com/sisatech/toml"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vkern"
)

type vorteildConf struct {
	KernelSources struct {
		Directory          string   `toml:"directory"`
		DropPath           string   `toml:"drop-path"`
		RemoteRepositories []string `toml:"remote-repositories"`
	} `toml:"kernel-sources"`
}

var ksrc vkern.Manager

type vorteilConfig struct {
	kernels string
	watch   string
	sources []string
}

// loadVorteilConfig : Load vorteil config from ~/.vorteild path.
//	If this path fails to load, load defaults instead.
//	Return vorteilConfig Objects with loaded config values
func loadVorteilConfig() (vorteilConfig, error) {
	var vCfg vorteilConfig

	home, err := homedir.Dir()
	if err != nil {
		return vCfg, err
	}

	vorteild := filepath.Join(home, ".vorteil")
	conf := filepath.Join(vorteild, "conf.toml")

	confData, err := ioutil.ReadFile(conf)
	if err != nil {
		vCfg.kernels = filepath.Join(vorteild, "kernels")
		vCfg.watch = filepath.Join(vCfg.kernels, "watch")
		vCfg.sources = []string{"https://downloads.vorteil.io/kernels"}
	} else {
		vconf := new(vorteildConf)
		err = toml.Unmarshal(confData, vconf)
		if err != nil {
			return vCfg, err
		}
		vCfg.kernels = vconf.KernelSources.Directory
		vCfg.watch = vconf.KernelSources.DropPath
		vCfg.sources = vconf.KernelSources.RemoteRepositories
	}

	return vCfg, nil
}

// mkDirAllSlice - Utils : Create the directories in the 'dirs' slice with permissions of 'perm'
func mkDirAllSlice(perm os.FileMode, dirs ...string) error {
	for _, dir := range dirs {
		err := os.MkdirAll(dir, perm)
		if err != nil {
			return err
		}
	}

	return nil
}

func initKernels() error {
	vCfg, err := loadVorteilConfig()
	if err != nil {
		return err
	}

	err = mkDirAllSlice(0777, vCfg.kernels, vCfg.watch)
	if err != nil {
		return err
	}

	ksrc, err = vkern.CLI(vkern.CLIArgs{
		Directory:          vCfg.kernels,
		DropPath:           vCfg.watch,
		RemoteRepositories: vCfg.sources,
	}, log)
	if err != nil {
		return err
	}

	vkern.Global = ksrc
	vimg.GetKernel = ksrc.Get
	vimg.GetLatestKernel = vkern.ConstructGetLastestKernelsFunc(&ksrc)

	return nil

}
