package cli

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 **/ //
import (
	"bytes"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vpkg"
)

var repositoriesCmd = &cobra.Command{
	Use:   "repositories",
	Short: "Interact with vorteil repositories",
}

var keysCmd = &cobra.Command{
	Use:   "keys",
	Short: "Create, List and Delete keys for authentication with Vorteil Repositories",
}

func checkKeysFolder() (string, error) {
	usr, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	pathCheck := filepath.Join(usr, ".vorteil", "repository-keys")
	if _, err = os.Stat(pathCheck); os.IsNotExist(err) {
		return "", errors.New("no keys found")
	}
	return pathCheck, nil
}

var defaultKeyCmd = &cobra.Command{
	Use:   "default KEY_NAME",
	Short: "View the keys that are equal to the default or change the default repository key by providing KEY_NAME",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		usr, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		pathCheck := filepath.Join(usr, ".vorteil", "repository-keys")
		_, err = os.Stat(pathCheck)
		if err != nil {
			err = os.MkdirAll(pathCheck, os.ModePerm)
			if err != nil {
				return err
			}
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {

		pathCheck, err := checkKeysFolder()
		if err != nil {
			SetError(err, 1)
			return
		}

		if len(args) > 0 {
			// Set default to args
			key := args[0]
			f, err := os.Open(filepath.Join(pathCheck, key))
			if err != nil {
				SetError(fmt.Errorf("%s does not exist as a key stored", key), 3)
				return
			}
			defer f.Close()
			data, err := ioutil.ReadAll(f)
			if err != nil {
				SetError(fmt.Errorf("unable to read from key file"), 4)
				return
			}

			defaultF, err := os.OpenFile(filepath.Join(pathCheck, "default"), os.O_RDWR|os.O_CREATE, 0644)
			if err != nil {
				SetError(fmt.Errorf("unable to open default key: %s", err.Error()), 5)
				return
			}

			defer defaultF.Close()
			err = ioutil.WriteFile(filepath.Join(pathCheck, "default"), data, os.ModePerm)
			if err != nil {
				SetError(err, 6)
				return
			}

			// Finished doing what we wanted
			return
		}

		// Open default
		f, err := os.Open(filepath.Join(pathCheck, "default"))
		if err != nil {
			SetError(errors.New("default key has not been set"), 6)
			return
		}
		defer f.Close()

		h := md5.New()
		if _, err := io.Copy(h, f); err != nil {
			SetError(err, 7)
			return
		}

		fis, err := ioutil.ReadDir(pathCheck)
		if err != nil {
			SetError(err, 8)
			return
		}

		for _, fi := range fis {

			if fi.Name() != "default" {
				f2, err := os.Open(filepath.Join(pathCheck, fi.Name()))
				if err != nil {
					SetError(err, 9)
					return
				}
				h2 := md5.New()
				if _, err := io.Copy(h2, f2); err != nil {
					SetError(err, 6)
					return
				}
				if bytes.Equal(h.Sum(nil), h2.Sum(nil)) {
					fmt.Println(fi.Name())
				}
				f2.Close()
			}

		}
	},
}

var createKeyCmd = &cobra.Command{
	Use:   "create NAME TOKEN",
	Short: "Creates a file containing the access token to be referenced using name",
	Args:  cobra.MaximumNArgs(2),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 2 {
			return errors.New("must provide a NAME and TOKEN")
		}
		usr, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		pathCheck := filepath.Join(usr, ".vorteil", "repository-keys")
		_, err = os.Stat(pathCheck)
		if err != nil {
			err = os.MkdirAll(pathCheck, os.ModePerm)
			if err != nil {
				return err
			}
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if name == "default" {
			SetError(errors.New("default is a reserved word and can't be a name"), 1)
			return
		}
		key := args[1]

		pathCheck, err := checkKeysFolder()
		if err != nil {
			SetError(err, 2)
			return
		}

		// Check if file exists
		// if stat returns no error return error saying you need to provide the force flag
		fi, err := os.Stat(filepath.Join(pathCheck, name))
		if err == nil && !flagForce {
			SetError(errors.New("key file already exists provide --force to overwrite"), 2)
			return
		}

		if flagForce && fi != nil {

			// open old file
			f2, err := os.Open(filepath.Join(pathCheck, name))
			if err != nil {
				SetError(err, 4)
				return
			}
			defer f2.Close()

			odata, err := ioutil.ReadAll(f2)
			if err != nil {
				SetError(err, 5)
				return
			}

			// check if []byte(key) is equal to default
			f, err := os.Open(filepath.Join(pathCheck, "default"))
			if err == nil {
				defer f.Close()
				data, err := ioutil.ReadAll(f)
				if err != nil {
					SetError(err, 6)
					return
				}
				if string(data) == string(odata) {
					// Write default to be the same
					err = ioutil.WriteFile(filepath.Join(pathCheck, "default"), []byte(key), os.ModePerm)
					if err != nil {
						SetError(err, 7)
						return
					}
				}
			}
		}

		// Write key to a file under that keys directory
		err = ioutil.WriteFile(filepath.Join(pathCheck, name), []byte(key), os.ModePerm)
		if err != nil {
			SetError(err, 8)
			return
		}

		// Check if default and write another file called default under repository-keys
		if flagDefault {
			err = ioutil.WriteFile(filepath.Join(pathCheck, "default"), []byte(key), os.ModePerm)
			if err != nil {
				SetError(err, 9)
				return
			}
		}
	},
}

func init() {
	f := createKeyCmd.Flags()
	f.BoolVar(&flagDefault, "default", false, "save this key to use as default")
	f.BoolVar(&flagForce, "force", false, "force overwrite of key file")
}

var listKeysCmd = &cobra.Command{
	Use:   "list",
	Short: "List all keys currently stored",
	Args:  cobra.MaximumNArgs(0),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		usr, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		pathCheck := filepath.Join(usr, ".vorteil", "repository-keys")
		_, err = os.Stat(pathCheck)
		if err != nil {
			err = os.MkdirAll(pathCheck, os.ModePerm)
			if err != nil {
				return err
			}
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		pathCheck, err := checkKeysFolder()
		if err != nil {
			SetError(err, 2)
			return
		}

		fis, err := ioutil.ReadDir(pathCheck)
		if err != nil {
			SetError(err, 2)
			return
		}

		var data []byte

		// Open default key to check which one is also the same as default
		defaultKey, err := os.Open(filepath.Join(pathCheck, "default"))
		if err == nil {
			defer defaultKey.Close()
			data, err = ioutil.ReadAll(defaultKey)
			if err != nil {
				SetError(err, 3)
				return
			}
		}

		for _, fi := range fis {
			if fi.Name() != "default" {
				if len(data) > 0 {
					f, err := os.Open(filepath.Join(pathCheck, fi.Name()))
					if err != nil {
						SetError(err, 4)
						return
					}
					defer f.Close()
					keyD, err := ioutil.ReadAll(f)
					if err != nil {
						SetError(err, 5)
						return
					}
					if string(data) == string(keyD) {
						fmt.Printf("%s [default]\n", fi.Name())
						continue
					}
				}
				fmt.Println(fi.Name())
			}
		}
	},
}

var deleteKeyCmd = &cobra.Command{
	Use:   "delete NAME",
	Short: "Delete a key currently stored",
	Args:  cobra.MaximumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return errors.New("Must provide the name of the key you want to delete")
		}
		usr, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		pathCheck := filepath.Join(usr, ".vorteil", "repository-keys")
		_, err = os.Stat(pathCheck)
		if err != nil {
			err = os.MkdirAll(pathCheck, os.ModePerm)
			if err != nil {
				return err
			}
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if name == "default" {
			SetError(errors.New("default is a reserved word and can't be used to delete a key"), 1)
			return
		}
		pathCheck, err := checkKeysFolder()
		if err != nil {
			SetError(err, 2)
			return
		}
		path := filepath.Join(pathCheck, name)
		dpath := filepath.Join(pathCheck, "default")

		// before removing we should check if default is the same and delete that
		f1, err := ioutil.ReadFile(path)
		if err != nil {
			SetError(fmt.Errorf("%s keyfile does not exist", name), 2)
			return
		}

		f2, err := ioutil.ReadFile(dpath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				SetError(err, 3)
				return
			}
		}

		// If same bytes remove default aswell
		if bytes.Equal(f1, f2) {
			err = os.Remove(dpath)
			if err != nil {
				SetError(err, 4)
				return
			}
		}

		// Else just remove the keyfile
		err = os.Remove(filepath.Join(pathCheck, name))
		if err != nil {
			SetError(err, 5)
			return
		}

	},
}

var pushCmd = &cobra.Command{
	Use:   "push REPOSITORY ORG/BUCKET/APP SOURCE",
	Short: "Push to a repository",
	Long:  `The push command is a function for quickly pushing an application to the repository.`,
	Args:  cobra.MaximumNArgs(3),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 3 {
			return errors.New("must provide three arguments <REPOSITORY ORG/BUCKET/APP SOURCE>")
		}
		words := strings.Split(args[1], "/")
		if len(words) < 3 {
			return fmt.Errorf("invalid format for <org/bucket/app> argument")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {

		urlPath := args[0]
		repoPath := strings.Split(args[1], "/")
		buildablePath := args[2]

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

		err = pushPackage(pkgBuilder, urlPath, repoPath)
		if err != nil {
			SetError(err, 5)
			return
		}

		return
	},
}

func init() {
	f := pushCmd.Flags()
	f.StringVarP(&flagKey, "key", "k", "", "vrepo authentication key file name")
}

// checkAuthentication checks to see if the flag has provided a name if not
// checks to see if default exists if that doesnt exist
// errors out saying you need to provide authentication
func checkAuthentication() (string, error) {
	pathCheck, err := checkKeysFolder()
	if err != nil {
		return "", err
	}
	token, err := checkDefaultAndProvided(pathCheck)
	if err != nil {
		return "", err
	}

	return token, nil
}

// checkDefaultAndProvided checks both files for authentication
func checkDefaultAndProvided(pathCheck string) (string, error) {
	var token string
	var path string
	var err error
	if flagKey != "" {
		path = filepath.Join(pathCheck, flagKey)
		token, err = checkAuthFile(path)
		if err != nil {
			return "", fmt.Errorf("unable to locate '%s' keyfile", flagKey)
		}
	} else {
		path = filepath.Join(pathCheck, "default")
		token, err = checkAuthFile(path)
		if err != nil {
			return "", errors.New("unable to find any authentication provided try using the --key flag")
		}
	}
	return token, nil
}

// checkAuthFile checks if file exists returns token or error
func checkAuthFile(path string) (string, error) {
	_, err := os.Stat(path)
	if err == nil {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		bytes, err := ioutil.ReadAll(f)
		if err != nil {
			return "", err
		}
		return string(bytes), nil
	}
	return "", fmt.Errorf("unable to find keyfile at %s", path)
}

// preparePackage for upload
func preparePackage(builder vpkg.Builder) (*os.File, error) {
	spinner := log.NewProgress("Preparing Package", "", 0)
	defer spinner.Finish(true)
	file, err := ioutil.TempFile(os.TempDir(), "vpkg-")
	if err != nil {
		return nil, err
	}

	err = builder.Pack(file)
	if err != nil {
		return nil, err
	}

	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}

	return file, nil
}

// generateRequest creates request to send to the repository
func generateRequest(url string, repo []string, r io.ReadCloser, token string) (*http.Request, error) {
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/organisations/%s/buckets/%s/apps/%s", url, repo[0], repo[1], repo[2]), r)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))

	return req, nil
}

// uploadPackage sends the request to upload package
func uploadPackage(url string, repo []string, token string, file *os.File) error {
	client := &http.Client{}

	stats, err := file.Stat()
	if err != nil {
		return err
	}

	p := log.NewProgress("Uploading Package", "KiB", stats.Size())
	r := p.ProxyReader(file)
	defer p.Finish(true)

	req, err := generateRequest(url, repo, r, token)
	if err != nil {
		p.Finish(false)
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		p.Finish(false)
		return err
	}

	defer resp.Body.Close()

	// If content-type is text/html its hitting a website and not an api
	if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return errors.New("not a valid vorteil repository url")
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			p.Finish(false)
			return err
		}
		return fmt.Errorf("%s: %s", errors.New(resp.Status), string(bodyBytes))
	}
	return nil
}

// pushPackage takes builder, url and repo array of strings which is org/bucket/app
func pushPackage(builder vpkg.Builder, url string, repo []string) error {

	// check authentication before doing things
	token, err := checkAuthentication()
	if err != nil {
		return err
	}

	if isVrepo, _ := checkIfNewVRepo(url); isVrepo == "" {
		return fmt.Errorf("target repo '%s' is not a Vorteil Repository", url)
	}

	file, err := preparePackage(builder)
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())

	err = uploadPackage(url, repo, token, file)
	if err != nil {
		return err
	}

	return nil
}
