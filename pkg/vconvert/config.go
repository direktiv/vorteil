package vconvert

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"

	"github.com/vorteil/vorteil/pkg/elog"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/viper"
)

const (
	configFileName = "vconvert.yaml"
)

// reads in config file, uses defaults if not found
func initConfig(cfgFile string, log elog.View) {

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
			log.Debugf("%s", err.Error())
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
