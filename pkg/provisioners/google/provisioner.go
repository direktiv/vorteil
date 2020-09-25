/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/vorteil/vorteil/pkg/provisioners"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

// ProvisionerType : Constant string value used to represent the provisioner type for google
const ProvisionerType = "google-compute"

// Provisioner satisfies the provisioners.Provisioner interface
type Provisioner struct {
	cfg *Config
}

// Config contains configuration fields required by the Provisioner
type Config struct {
	Bucket string `json:"bucket"` // Name of the bucket
	Key    string `json:"key"`    // base64 encoded contents of a (JSON) Google Cloud Platform service account key file
}

// Create a provisioner object
func Create(cfg *Config) (provisioners.Provisioner, error) {

	p := &Provisioner{
		cfg: cfg,
	}

	err := p.init()
	if err != nil {
		return nil, err
	}

	return p, nil
}

// Type returns 'google-compute'
func (p *Provisioner) Type() string {
	return ProvisionerType
}

// DiskFormat returns the provisioners required disk format
func (p *Provisioner) DiskFormat() vdisk.Format {
	return vdisk.GCPFArchiveFormat
}

func (p *Provisioner) SizeAlign() vcfg.Bytes {
	return vcfg.GiB
}

func (p *Provisioner) Initialize(data []byte) error {

	cfg := new(Config)
	err := json.Unmarshal(data, cfg)
	if err != nil {
		return err
	}

	p.cfg = cfg
	err = p.init()
	if err != nil {
		return err
	}

	return nil
}

var scopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
}

func (p *Provisioner) init() error {
	// Delcare vars
	var err error
	var key []byte
	var oauthToken *jwt.Config
	var storageClient *storage.Client
	var bucketHandler *storage.BucketHandle

	// Using the values stored within p.cfg,
	// attempt to verify that the provisioner
	// is valid

	key, err = base64.StdEncoding.DecodeString(p.cfg.Key)
	if err != nil {
		goto ERROR
	}

	oauthToken, err = google.JWTConfigFromJSON(key, scopes...)
	if err != nil {
		goto ERROR
	}

	storageClient, err = storage.NewClient(context.Background(), option.WithCredentialsJSON(key))
	if err != nil {
		goto ERROR
	}
	defer storageClient.Close()

	bucketHandler = storageClient.Bucket(p.cfg.Bucket)
	_, err = bucketHandler.Attrs(context.Background())
	if err != nil {
		goto ERROR
	}

	_, err = compute.New(oauthToken.Client(context.Background()))
	if err != nil {
		goto ERROR
	}

	return nil

ERROR:
	return err
}

// Provision ...
func (p *Provisioner) Provision(args *provisioners.ProvisionArgs) error {

	key, err := base64.StdEncoding.DecodeString(p.cfg.Key)
	if err != nil {
		return err
	}

	keyMap := make(map[string]interface{})
	err = json.Unmarshal(key, &keyMap)
	if err != nil {
		return err
	}

	x, ok := keyMap["project_id"]
	if !ok {
		err = fmt.Errorf("'project_id' field not found in credentials")
		return err
	}

	projectID, ok := x.(string)
	if !ok {
		err = fmt.Errorf("unable to interpret 'project_id' field as string")
		return err
	}

	oauthToken, err := google.JWTConfigFromJSON(key, scopes...)
	if err != nil {
		return err
	}

	args.Logger.Infof("Initializing storage client...")
	storageClient, err := storage.NewClient(context.Background(), option.WithCredentialsJSON(key))
	if err != nil {
		return err
	}
	defer storageClient.Close()
	args.Logger.Infof(" done.")

	args.Logger.Infof("Initializing compute client...")
	computeClient, err := compute.New(oauthToken.Client(args.Context))
	if err != nil {
		return err
	}
	args.Logger.Infof(" done.")

	_, err = computeClient.Images.Get(projectID, args.Name).Do()
	if err == nil {
		if !args.Force {
			err = fmt.Errorf("image '%s' already exists", args.Name)
			return err
		}
	}

	bucketHandler := storageClient.Bucket(p.cfg.Bucket)

	name := strings.Replace(fmt.Sprintf("%s.tar.gz", uuid.New().String()), "-", "", -1)
	obj := bucketHandler.Object(name)
	_, err = obj.Attrs(args.Context)
	if err == nil {
		err = fmt.Errorf("object '%s' already exists", name)
		return err
	}

	args.Logger.Infof("Uploading disk image...")
	w := obj.NewWriter(args.Context)
	err = func() error {
		defer func() {
			w.Close()
		}()

		_, err = io.Copy(w, args.Image)
		if err != nil {
			return err
		}

		err = w.Close()
		if err != nil {
			return err
		}

		return nil
	}()
	if err != nil {
		return err
	}
	args.Logger.Infof(" done.")
	defer func() {
		err = obj.Delete(args.Context)
	}()

	var pollTimeout int

	if args.Force {
		args.Logger.Infof("Finding and deleting conflicting image...")
		imagesForce := computeClient.Images.List(projectID)
		list, err := imagesForce.Do()
		if err != nil {
			return err
		}

		for _, image := range list.Items {
			if image.Name == args.Name {
				delOp, err := computeClient.Images.Delete(projectID, image.Name).Do()
				if err != nil {
					return err
				}
				for delOp.Status != "DONE" && pollTimeout <= 120 {
					delOp, err = computeClient.GlobalOperations.Get(projectID, delOp.Name).Do()
					if err != nil {
						return err
					}

					if delOp.Status == "DONE" {
						break
					}

					time.Sleep(time.Second)
					pollTimeout++
				}
				if pollTimeout >= 120 {
					return fmt.Errorf("timed out waiting for image deletion")
				}
				break
			}
		}

		args.Logger.Infof(" done.")
	}

	args.Logger.Infof("Creating image...")
	op, err := computeClient.Images.Insert(projectID, &compute.Image{
		Name: args.Name,
		RawDisk: &compute.ImageRawDisk{
			Source: fmt.Sprintf("https://storage.googleapis.com/%s/%s", p.cfg.Bucket, name),
		},
		Description: args.Description,
	}).Do()
	if err != nil {
		return err
	}

	for op.Status != "DONE" && pollTimeout <= 120 {
		op, err = computeClient.GlobalOperations.Get(projectID, op.Name).Do()
		if err != nil {
			return err
		}

		if op.Status == "DONE" {
			args.Logger.Infof(" done.")
			break
		}

		time.Sleep(time.Second)
		pollTimeout++
	}
	if pollTimeout >= 120 {
		return fmt.Errorf("timed out waiting for image creation")
	}

	return nil
}

// Marshal returns json provisioner as bytes
func (p *Provisioner) Marshal() ([]byte, error) {

	m := make(map[string]interface{})
	m[provisioners.MapKey] = ProvisionerType
	m["bucket"] = p.cfg.Bucket
	m["key"] = p.cfg.Key

	out, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	return out, nil
}
