package azure

import (
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
	"github.com/vorteil/vorteil/pkg/provisioners"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
)

const ProvisionerType = "microsoft-azure"

// Provisioner satisfies the provisioners.Provisioner interface
type Provisioner struct {
	cfg *Config

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
	Key                string `json:"key"`                // Base64 encoded contents of an Azure service pricipal credentials file
	Location           string `json:"location"`           // Azure location
	ResourceGroup      string `json:"resourceGroup"`      // Azure resource group name
	StorageAccountKey  string `json:"storageAccountKey"`  // Azure storage account key
	StorageAccountName string `json:"storageAccountName"` // Azure storage account name
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
	return vdisk.VHDFormat
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

func (p *Provisioner) init() error {

	var err error

	p.credentials, err = base64.StdEncoding.DecodeString(p.cfg.Key)
	if err != nil {
		return err
	}

	keyMap := make(map[string]interface{})
	err = json.Unmarshal(p.credentials, &keyMap)
	if err != nil {
		return err
	}

	x, ok := keyMap["subscriptionId"]
	if !ok {
		return fmt.Errorf("'subscriptionId' field not found in credentials")
	}

	subID, ok := x.(string)
	if !ok {
		return fmt.Errorf("unable to interpret 'project_id' field as string")
	}

	x, ok = keyMap["clientId"]
	if ok {
		str, ok := x.(string)
		if ok {
			p.clientID = str
		}
	}
	x, ok = keyMap["tenantId"]
	if ok {
		str, ok := x.(string)
		if ok {
			p.tenantID = str
		}
	}
	x, ok = keyMap["clientSecret"]
	if ok {
		str, ok := x.(string)
		if ok {
			p.clientSecret = str
		}
	}
	x, ok = keyMap["resourceManagerEndpointUrl"]
	if ok {
		str, ok := x.(string)
		if ok {
			p.resourceManagerEndpointURL = str
		}
	}

	p.subscriptionID = subID
	return nil

}

func (p *Provisioner) SizeAlign() vcfg.Bytes {
	return vcfg.Bytes(0)
}

func (p *Provisioner) Provision(args *provisioners.ProvisionArgs) error {

	creds, err := azblob.NewSharedKeyCredential(p.cfg.StorageAccountName, p.cfg.StorageAccountKey)
	if err != nil {
		return err
	}
	pi := azblob.NewPipeline(creds, azblob.PipelineOptions{})

	url, err := url.Parse(
		fmt.Sprintf("https://%s.blob.core.windows.net/%s",
			p.cfg.StorageAccountName, p.cfg.Container))
	if err != nil {
		return err
	}

	containerURL := azblob.NewContainerURL(*url, pi)
	ctx := context.Background()

	containerURL.Create(ctx, azblob.Metadata{}, azblob.PublicAccessNone) // creates if not exists

	storageClient, err := storage.NewBasicClient(p.cfg.StorageAccountName, p.cfg.StorageAccountKey)
	if err != nil {
		return err
	}

	x := storageClient.GetBlobService()
	container := x.GetContainerReference(p.cfg.Container)

	remoteDiskName := fmt.Sprintf("%s.vhd", strings.TrimSuffix(args.Name, ".vhd"))
	blob := container.GetBlobReference(remoteDiskName)

	if args.Force {
		_, err = blob.DeleteIfExists(&storage.DeleteBlobOptions{})
		if err != nil {
			return err
		}

	}

	imagesClient := compute.NewImagesClient(p.subscriptionID)
	settings, err := auth.GetSettingsFromEnvironment()
	if err != nil {
		return err
	}

	settings.Values[auth.SubscriptionID] = p.subscriptionID
	settings.Values[auth.ResourceManagerEndpoint] = azure.PublicCloud.ResourceManagerEndpoint
	settings.Values[auth.ClientID] = p.clientID
	settings.Values[auth.ClientSecret] = p.clientSecret
	settings.Values[auth.Resource] = p.resourceManagerEndpointURL
	settings.Values[auth.TenantID] = p.tenantID

	imagesClient.Authorizer, err = settings.GetAuthorizer()
	if err != nil {
		return err
	}

	forced := args.Force
	result, err := imagesClient.Get(args.Context, p.cfg.ResourceGroup, args.Name, "")
	if err == nil || result.ID != nil {
		// image already exists
		if !forced {
			// o.updateStatus("Image already exists; aborting. To replace conflicting image, include the 'force' directive.")
			return fmt.Errorf("Image already exists; aborting. To replace conflicting image, include the 'force' directive.")
		}

		// o.updateStatus("Deleting existing image.")
		delFuture, err := imagesClient.Delete(args.Context, p.cfg.ResourceGroup, args.Name)
		if err != nil {
			return err
		}

		err = delFuture.WaitForCompletionRef(args.Context, imagesClient.Client)
		if err != nil {
			return err
		}
	}

	f, err := ioutil.TempFile(os.TempDir(), "")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()

	_, err = io.Copy(f, args.Image)
	if err != nil {
		return err
	}

	err = f.Close()
	if err != nil {
		return err
	}

	stat, err := os.Stat(f.Name())
	if err != nil {
		return err
	}

	length := stat.Size()

	blob.Properties.ContentType = "text/plain"
	blob.Properties.ContentLength = length

	err = blob.PutPageBlob(nil)
	if err != nil {
		return err
	}

	rBytes, err := ioutil.ReadFile(f.Name())
	if err != nil {
		return err
	}

	i := 0
	for i = 0; i < (int(length) - 4194304); i += 4194304 {
		data := make([]byte, 4194304)
		copy(data[:], rBytes[i:i+4194304])
		br := storage.BlobRange{
			Start: uint64(i),
			End:   uint64(i + 4194304 - 1),
		}

		var uploadSegment bool
		for _, d := range data {
			if d != byte(0) {
				uploadSegment = true
				break
			}
		}

		if uploadSegment {
			err = blob.WriteRange(br, bytes.NewReader(data), nil)
			if err != nil {
				return err
			}
		}
		// o.updateStatus(fmt.Sprintf("Uploading: %.1f%s", (float64(i)+float64(len(data)))/float64(length)*100, "%"))
	}

	rem := length - int64(i)
	data := make([]byte, rem)
	copy(data[:], rBytes[i:length])
	br := storage.BlobRange{
		Start: uint64(i),
		End:   uint64(length) - 1,
	}

	err = blob.WriteRange(br, bytes.NewReader(data), nil)
	if err != nil {
		return err
	}

	// o.updateStatus(fmt.Sprint("Uploading: 100%"))

	g := int32(1024 * 1024 * 1024)
	gigs := int32(length) / g
	gigsmod := int32(length) % g
	if gigsmod != 0 {
		gigs++
	}
	diskSize := gigs

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

	// o.updateStatus("Sent request to create image from storage object.")
	future, err := imagesClient.CreateOrUpdate(args.Context, p.cfg.ResourceGroup, args.Name, *img)
	if err != nil {
		return err
	}

	// o.updateStatus("Waiting for completion.")
	err = future.WaitForCompletionRef(args.Context, imagesClient.Client)
	if err != nil {
		return err
	}

	// o.updateStatus("Done!")

	return nil
}

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
