package cli

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vpkg"
)

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 **/ //

var pushCmd = &cobra.Command{
	Use:   "push [SOURCE] [URL]",
	Short: "Push to a repository",
	Long:  `The push command is a function for quickly pushing an application to the repository.`,
	Args:  cobra.MaximumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		urlPath := ""
		buildablePath := "."
		if len(args) >= 2 {
			buildablePath = args[0]
			urlPath = args[1]
		}

		pkgBuilder, err := getPackageBuilder("BUILDABLE", buildablePath)
		if err != nil {
			SetError(err, 2)
			return
		}
		defer pkgBuilder.Close()

		err = modifyPackageBuilder(pkgBuilder)
		if err != nil {
			SetError(err, 3)
			return
		}

		err = pushPackage(pkgBuilder, urlPath)
		if err != nil {
			SetError(err, 4)
			return
		}

		return
	},
}

func init() {
	f := pushCmd.Flags()
	f.StringVar(&flagName, "name", "", "name the application you want to push")
	f.StringVarP(&flagKey, "key", "k", "", "vrepo authentication key")
	f.StringVarP(&pushOrganisation, "org", "o", "", "the organisation the application will be under")
	f.StringVarP(&pushBucket, "bucket", "b", "", "the bucket the application will be under")
}

func pushPackage(builder vpkg.Builder, url string) error {

	if pushBucket == "" {
		return errors.New("Please provide the --bucket flag to push an application")
	}
	if pushOrganisation == "" {
		return errors.New("Please provide the --org flag to push an application")
	}
	if flagName == "" {
		return errors.New("Please provide the --name flag to push an application")
	}
	if flagKey == "" {
		return errors.New("Please provide the --key flag to authenticate with the repository")
	}

	file, err := ioutil.TempFile(os.TempDir(), "vpkg-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())

	stats, err := file.Stat()
	if err != nil {
		return err
	}

	err = builder.Pack(file)
	if err != nil {
		return err
	}

	client := &http.Client{}
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/organisations/%s/buckets/%s/apps/%s", url, pushOrganisation, pushBucket, flagName), file)
	if err != nil {
		return err
	}

	if flagKey != "" {
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", flagKey))
	}

	p := log.NewProgress("Downloading package", "KiB", stats.Size())

	p.ProxyReader(file)
	defer p.Finish(false)
	// spinner := log.NewProgress("Uploading Package", "", 0)
	// defer spinner.Finish(true)

	resp, err := client.Do(req)
	if err != nil {
		resp.Body.Close()
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}

	return nil
}
