package google

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"cloud.google.com/go/storage"
	"github.com/google/uuid"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/provisioners"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"golang.org/x/oauth2/google"
	"golang.org/x/oauth2/jwt"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/option"
)

// MapKey ...
var MapKey = ""

// Provisioner ...
type Provisioner struct {
	cfg *Config
	log elog.View

	storageClient *storage.Client
	bucketHandle  *storage.BucketHandle
	keyMap        map[string]interface{}
	computeClient *compute.Service
	jsonKey       []byte
}

// ProvisionerArgs TODO:
type ProvisionerArgs struct {
	Logger elog.View
	Data   []byte
}

// Config contains configuration fields required by the Provisioner
type Config struct {
	Bucket string `json:"bucket"` // Name of the bucket
	Key    string `json:"key"`    // base64 encoded contents of a (JSON) Google Cloud Platform service account key file
}

// ProvisionArgs TODO:
type ProvisionArgs struct {
	Name            string
	Description     string
	Force           bool
	ReadyWhenUsable bool
}

const (
	// ProvisionerType for GCP provisioner
	ProvisionerType = "google-compute"
	statusDone      = "DONE"
	waitInSecs      = 120
)

var scopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
}

// Validate Provisioner is valid by dry running creating google clients
func (p *Provisioner) Validate() error {
	defer p.closeClients()
	err := p.createClients()
	// if err != nil {
	// 	err = fmt.Errorf("invalid provisioner, %v", err)
	// }
	return &provisioners.InvalidProvisionerError{
		Err: err,
	}
}

// createClients
func (p *Provisioner) createClients() error {

	var (
		err        error
		oauthToken *jwt.Config
		key        []byte
	)

	// Using the values stored within p.cfg,
	// attempt to verify that the provisioner
	// is valid
	key, err = base64.StdEncoding.DecodeString(p.cfg.Key)
	if err != nil {
		err = fmt.Errorf("could not decode key of length %v: %v", len(p.cfg.Key), err)
		return err
	}

	p.jsonKey, _ = base64.StdEncoding.DecodeString(p.cfg.Key)

	p.keyMap = make(map[string]interface{})
	err = json.Unmarshal(key, &p.keyMap)
	if err != nil {
		err = fmt.Errorf("could not unmarshal key: %v", err)
		return err
	}

	p.storageClient, err = storage.NewClient(context.Background(), option.WithCredentialsJSON(key))
	if err != nil {
		return err
	}

	p.bucketHandle = p.storageClient.Bucket(p.cfg.Bucket)
	_, err = p.bucketHandle.Attrs(context.Background())
	if err != nil {
		return err
	}

	oauthToken, err = google.JWTConfigFromJSON(key, scopes...)
	if err != nil {
		return err
	}

	p.computeClient, err = compute.New(oauthToken.Client(context.Background()))

	return err
}

func (p *Provisioner) closeClients() {
	p.bucketHandle = nil
	p.computeClient = nil
	p.keyMap = nil
	p.jsonKey = nil
	if p.storageClient != nil {
		p.storageClient.Close()
		p.storageClient = nil
	}
}

// NewProvisioner TODO:
func NewProvisioner(args *ProvisionerArgs) (*Provisioner, error) {
	p := new(Provisioner)
	p.cfg = new(Config)
	p.log = args.Logger
	err := json.Unmarshal(args.Data, p.cfg)
	if err != nil {
		return nil, err
	}

	err = p.Validate()

	return p, err
}

// Type returns 'google-compute'
func (p *Provisioner) Type() string {
	return ProvisionerType
}

// DiskFormat returns the provisioners required disk format
func (p *Provisioner) DiskFormat() vdisk.Format {
	return vdisk.GCPFArchiveFormat
}

// SizeAlign returns vcfg GiB size in bytes
func (p *Provisioner) SizeAlign() vcfg.Bytes {
	return vcfg.GiB
}

// Provision provisions BUILDABLE to GCP
func (p *Provisioner) Provision(args *provisioners.ProvisionArgs) error {
	p.log.Printf("Hello World Im Google")

	defer p.closeClients()
	err := p.createClients()
	if err != nil {
		return fmt.Errorf("failed to create client for google, error: %v", err)
	}

	projectID := p.keyMap["project_id"].(string)

	img, err := p.computeClient.Images.Get(projectID, args.Name).Do()
	if err == nil && !args.Force {
		return fmt.Errorf("image '%s' already exists", args.Name)
	}

	name := strings.Replace(fmt.Sprintf("%s.tar.gz", uuid.New().String()), "-", "", -1)
	obj := p.bucketHandle.Object(name)

	_, err = obj.Attrs(args.Context)
	if err == nil {
		return fmt.Errorf("object '%s' already exists", name)
	}

	w := obj.NewWriter(args.Context)

	progress := p.log.NewProgress(fmt.Sprintf("Uploading %s:", args.Name), "KiB", int64(args.Image.Size()))
	pr := progress.ProxyReader(args.Image)
	defer pr.Close()

	_, err = io.Copy(w, pr)
	if err != nil {
		progress.Finish(false)
		w.Close()
		return err
	}
	w.Close()
	progress.Finish(true)

	defer func() {
		obj.Delete(args.Context)
	}()

	if args.Force && img != nil {
		err := p.deleteConflictingImage(projectID, args.Name)
		if err != nil {
			return err
		}
	}

	return p.uploadImage(projectID, name, args)

	return nil
}

// Marshal returns json provisioner as bytes
func (p *Provisioner) Marshal() ([]byte, error) {
	return nil, nil
}

// utils
func (p *Provisioner) uploadImage(projectID, file string, args *provisioners.ProvisionArgs) error {

	ciprogree := p.log.NewProgress("Creating Image", "", 0)
	defer ciprogree.Finish(false)

	op, err := p.computeClient.Images.Insert(projectID, &compute.Image{
		Name: args.Name,
		RawDisk: &compute.ImageRawDisk{
			Source: fmt.Sprintf("https://storage.googleapis.com/%s/%s", p.cfg.Bucket, file),
		},
		Description: args.Description,
	}).Do()

	if err != nil {
		return err
	}

	var pollTimeout int
	for op.Status != statusDone && pollTimeout <= waitInSecs {
		<-time.After(time.Second)
		op, err = p.computeClient.GlobalOperations.Get(projectID, op.Name).Do()
		if err != nil {
			return err
		}
		pollTimeout++
	}

	if pollTimeout >= waitInSecs {
		return fmt.Errorf("timed out waiting for image creation")
	}

	return nil
}

func (p *Provisioner) deleteImage(projectID, name string) error {

	var (
		err   error
		delOp *compute.Operation
	)

	delOp, err = p.computeClient.Images.Delete(projectID, name).Do()
	if err != nil {
		return err
	}

	var pollTimeout int
	for delOp.Status != statusDone && pollTimeout <= waitInSecs {
		delOp, err = p.computeClient.GlobalOperations.Get(projectID, delOp.Name).Do()
		if err != nil {
			break
		}

		if delOp.Status == statusDone {
			break
		}

		time.Sleep(time.Second)
		pollTimeout++
	}
	if pollTimeout >= waitInSecs {
		return fmt.Errorf("timed out waiting for image deletion")
	}

	return nil
}

func (p *Provisioner) deleteConflictingImage(projectID, name string) error {

	var (
		err  error
		list *compute.ImageList
	)

	// args.Logger.Infof("Deleting conflicting image.")
	imagesForce := p.computeClient.Images.List(projectID)
	list, err = imagesForce.Do()
	if err != nil {
		return err
	}

	for _, image := range list.Items {
		if image.Name == name {
			err = p.deleteImage(projectID, name)
			break
		}
	}

	return err
}
