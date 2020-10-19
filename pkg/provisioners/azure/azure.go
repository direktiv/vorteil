package azure

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/profiles/latest/compute/mgmt/compute"
	"github.com/Azure/azure-sdk-for-go/storage"
	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/provisioners"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
)

const (
	// ProvisionerType : Constant string value used to represent the provisioner type azure
	ProvisionerType = "microsoft-azure"
	blobSize        = 4194304
)

// Provisioner satisfies the provisioners.Provisioner interface
type Provisioner struct {
	cfg *Config
	log elog.View

	credentials                []byte
	clientID                   string
	tenantID                   string
	clientSecret               string
	resourceManagerEndpointURL string
	subscriptionID             string
}

// Config contains configuration fields required by the Provisioner
type Config struct {
	Container          string `json:"container"`          // Azure storage container name
	Key                string `json:"key"`                // Base64 encoded contents of an Azure service principal credentials file
	Location           string `json:"location"`           // Azure location
	ResourceGroup      string `json:"resourceGroup"`      // Azure resource group name
	StorageAccountKey  string `json:"storageAccountKey"`  // Azure storage account key
	StorageAccountName string `json:"storageAccountName"` // Azure storage account name
}

// NewProvisioner - Create a Azure Provisioner object
func NewProvisioner(log elog.View, cfg *Config) (*Provisioner, error) {
	p := new(Provisioner)
	p.cfg = cfg
	p.log = log
	err := p.Validate()
	if err != nil {
		return nil, fmt.Errorf("invalid %s provisioner: %v", ProvisionerType, err)
	}

	return p, p.init()
}

// Validate ...
func (p *Provisioner) Validate() error {
	var err error

	if p.cfg.Container == "" {
		err = fmt.Errorf("no defined container")
	}

	if p.cfg.Key == "" {
		err = fmt.Errorf("no defined key")
	}

	if p.cfg.Location == "" {
		err = fmt.Errorf("no defined location")
	}

	if p.cfg.ResourceGroup == "" {
		err = fmt.Errorf("no defined resourceGroup")
	}

	if p.cfg.StorageAccountKey == "" {
		err = fmt.Errorf("no defined storageAccountKey")
	}

	if p.cfg.StorageAccountName == "" {
		err = fmt.Errorf("no defined storageAccountName")
	}

	return err
}

// Type returns 'microsoft-azure'
func (p *Provisioner) Type() string {
	return ProvisionerType
}

// DiskFormat returns the provisioners required disk format
func (p *Provisioner) DiskFormat() vdisk.Format {
	return vdisk.VHDFormat
}

func fetchVal(keyMap map[string]interface{}, name string) string {

	str, _ := keyMap[name].(string)
	return str

}

func (p *Provisioner) init() error {

	var err error

	p.credentials, err = base64.StdEncoding.DecodeString(p.cfg.Key)
	if err != nil {
		return fmt.Errorf("could not decode %s key: %v", ProvisionerType, err)
	}

	keyMap := make(map[string]interface{})
	err = json.Unmarshal(p.credentials, &keyMap)
	if err != nil {
		return fmt.Errorf("failed to unmarshal %s credentials: %v", ProvisionerType, err)
	}

	p.clientID = fetchVal(keyMap, "clientId")
	p.tenantID = fetchVal(keyMap, "tenantId")
	p.clientSecret = fetchVal(keyMap, "clientSecret")
	p.resourceManagerEndpointURL = fetchVal(keyMap, "resourceManagerEndpointUrl")
	p.subscriptionID = fetchVal(keyMap, "subscriptionId")

	return nil

}

// SizeAlign returns vcfg GiB size in bytes
func (p *Provisioner) SizeAlign() vcfg.Bytes {
	return vcfg.Bytes(0)
}

func (p *Provisioner) getBlobRef(name string) (*storage.Blob, error) {

	creds, err := azblob.NewSharedKeyCredential(p.cfg.StorageAccountName, p.cfg.StorageAccountKey)
	if err != nil {
		return nil, err
	}
	pi := azblob.NewPipeline(creds, azblob.PipelineOptions{})

	url, err := url.Parse(fmt.Sprintf("https://%s.blob.core.windows.net/%s",
		p.cfg.StorageAccountName, p.cfg.Container))
	if err != nil {
		return nil, err
	}

	containerURL := azblob.NewContainerURL(*url, pi)
	ctx := context.Background()

	containerURL.Create(ctx, azblob.Metadata{}, azblob.PublicAccessNone) // creates if not exists

	storageClient, err := storage.NewBasicClient(p.cfg.StorageAccountName, p.cfg.StorageAccountKey)
	if err != nil {
		return nil, err
	}

	x := storageClient.GetBlobService()
	container := x.GetContainerReference(p.cfg.Container)

	remoteDiskName := fmt.Sprintf("%s.vhd", strings.TrimSuffix(name, ".vhd"))
	return container.GetBlobReference(remoteDiskName), nil

}

func (p *Provisioner) getImagesClient() (compute.ImagesClient, error) {

	imagesClient := compute.NewImagesClient(p.subscriptionID)
	settings, err := auth.GetSettingsFromEnvironment()
	if err != nil {
		return imagesClient, err
	}

	settings.Values[auth.SubscriptionID] = p.subscriptionID
	settings.Values[auth.ResourceManagerEndpoint] = azure.PublicCloud.ResourceManagerEndpoint
	settings.Values[auth.ClientID] = p.clientID
	settings.Values[auth.ClientSecret] = p.clientSecret
	settings.Values[auth.Resource] = p.resourceManagerEndpointURL
	settings.Values[auth.TenantID] = p.tenantID

	imagesClient.Authorizer, err = settings.GetAuthorizer()

	return imagesClient, err

}

func bytesToGB(l int64) int32 {

	g := int64(1024 * 1024 * 1024)
	gigs := int64(l) / g

	if l%g != 0 {
		gigs++
	}

	return int32(gigs)

}

func prepTempFile(args *provisioners.ProvisionArgs) (*os.File, int64, error) {

	f, err := ioutil.TempFile(os.TempDir(), "")
	if err != nil {
		return nil, 0, err
	}

	n, err := io.Copy(f, args.Image)
	if err != nil {
		return nil, 0, err
	}

	return f, n, nil

}

func (p *Provisioner) uploadBlob(f *os.File, args *provisioners.ProvisionArgs, blob *storage.Blob) error {

	var ps int64

	if args.Force {
		_, err := blob.DeleteIfExists(&storage.DeleteBlobOptions{})
		if err != nil {
			return err
		}
	}

	progress := p.log.NewProgress(fmt.Sprintf("Uploading %s:", args.Name), "KiB", int64(args.Image.Size()))
	pr := progress.ProxyReader(f)
	defer pr.Close()

	err := blob.PutPageBlob(nil)
	if err != nil {
		return err
	}

	reader := bufio.NewReader(pr)
	buf := make([]byte, blobSize)

	f.Seek(0, 0)

	for {
		ps, _ = f.Seek(0, 1)
		n, err := reader.Read(buf)

		if err != nil {
			if err != io.EOF {
				progress.Finish(false)
				return err
			}
			break
		}

		br := storage.BlobRange{
			Start: uint64(ps),
			End:   uint64(int(ps) + (n - 1)),
		}

		// the last request might be smaller so we cut it off
		if n < blobSize {
			buf = buf[:n]
		}

		err = blob.WriteRange(br, bytes.NewReader(buf), nil)
		if err != nil {
			progress.Finish(false)
			return err
		}

	}
	progress.Finish(true)
	return nil

}

// Provision will provision the configured vorteil project to your configured gcp provisioner
func (p *Provisioner) Provision(args *provisioners.ProvisionArgs) error {

	var (
		length int64
		f      *os.File
	)

	blob, err := p.getBlobRef(args.Name)
	if err != nil {
		return err
	}

	f, length, err = prepTempFile(args)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())

	blob.Properties.ContentType = "text/plain"
	blob.Properties.ContentLength = length

	err = p.uploadBlob(f, args, blob)
	if err != nil {
		return err
	}

	err = p.createImage(length, args, blob)
	if err != nil {
		return err
	}

	return nil
}

func (p *Provisioner) deleteImageIfRequired(imagesClient compute.ImagesClient, args *provisioners.ProvisionArgs) error {

	result, err := imagesClient.Get(args.Context, p.cfg.ResourceGroup, args.Name, "")
	if err == nil || result.ID != nil {
		// image already exists
		if !args.Force {
			return fmt.Errorf("Image already exists; aborting. To replace conflicting image, include the 'force' directive")
		}

		ciprogree := p.log.NewProgress("Deleting existing image", "", 0)
		defer ciprogree.Finish(false)

		delFuture, err := imagesClient.Delete(args.Context, p.cfg.ResourceGroup, args.Name)
		if err != nil {
			return err
		}

		err = delFuture.WaitForCompletionRef(args.Context, imagesClient.Client)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Provisioner) createImage(length int64, args *provisioners.ProvisionArgs, blob *storage.Blob) error {

	imagesClient, err := p.getImagesClient()
	if err != nil {
		return err
	}

	err = p.deleteImageIfRequired(imagesClient, args)
	if err != nil {
		return err
	}

	ciprogree := p.log.NewProgress("Creating Image from blob", "", 0)
	defer ciprogree.Finish(false)

	diskSize := bytesToGB(length)
	img := new(compute.Image)
	img.ImageProperties = new(compute.ImageProperties)
	img.Location = &p.cfg.Location
	img.StorageProfile = new(compute.ImageStorageProfile)
	img.StorageProfile.OsDisk = new(compute.ImageOSDisk)
	img.StorageProfile.OsDisk.OsType = "Linux"
	img.StorageProfile.OsDisk.DiskSizeGB = &diskSize
	// set description as a tag
	tags := make(map[string]*string)
	tags["Description"] = &args.Description
	img.Tags = tags
	u := blob.GetURL()
	img.StorageProfile.OsDisk.BlobURI = &u
	img.HyperVGeneration = compute.HyperVGenerationTypesV1

	future, err := imagesClient.CreateOrUpdate(args.Context, p.cfg.ResourceGroup, args.Name, *img)
	if err != nil {
		return err
	}

	err = future.WaitForCompletionRef(args.Context, imagesClient.Client)
	if err != nil {
		return err
	}

	return nil

}

// Marshal returns json provisioner as bytes
func (p *Provisioner) Marshal() ([]byte, error) {

	m := make(map[string]interface{})
	m[provisioners.MapKey] = ProvisionerType
	m["key"] = p.cfg.Key
	m["container"] = p.cfg.Container
	m["location"] = p.cfg.Location
	m["resourceGroup"] = p.cfg.ResourceGroup
	m["storageAccountKey"] = p.cfg.StorageAccountKey
	m["storageAccountName"] = p.cfg.StorageAccountName

	out, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	return out, nil
}
