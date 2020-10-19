package cli

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/provisioners"
	"github.com/vorteil/vorteil/pkg/provisioners/amazon"
	"github.com/vorteil/vorteil/pkg/provisioners/azure"
	"github.com/vorteil/vorteil/pkg/provisioners/google"
	"github.com/vorteil/vorteil/pkg/provisioners/registry"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/vpkg"
)

var provisionCmd = &cobra.Command{
	Use:   "provision BUILDABLE PROVISIONER",
	Short: "Provision a vorteil buildable",
	Long: `Provision a vorteil buildable to a supported provisioner online.

Example Command:
 - Provisioning python3 package to an aws provisioner:
 $ vorteil images provision ./python3.vorteil ./awsProvisioner

PROVISIONER is a file that has been created with the 'vorteil provisioners new' command.
It tells vorteil where to provision your BUILDABLE to.

If your PROVISIONER was created with a passphrase you can input this passphrase with the
'--passphrase' flag when using the 'provision' command.`,
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		var provisionFile string
		provisionFile = args[1]

		// Load the provided provisioner file
		if _, err := os.Stat(provisionFile); err != nil {
			SetError(fmt.Errorf("Could not read PROVISIONER '%s' , error: %v", provisionFile, err), 1)
			return
		}

		b, err := ioutil.ReadFile(provisionFile)
		if err != nil {
			SetError(fmt.Errorf("Could not read PROVISIONER '%s' , error: %v", provisionFile, err), 2)
			return
		}

		data, err := provisioners.Decrypt(b, provisionPassPhrase)
		if err != nil {
			SetError(err, 3)
			return
		}

		ptype, err := provisioners.ProvisionerType(data)
		if err != nil {
			SetError(err, 4)
			return
		}

		prov, err := registry.NewProvisioner(ptype, log, data)
		if err != nil {
			SetError(err, 5)
			return
		}

		// var prov provisioners.Provisioner

		// switch ptype {
		// case google.ProvisionerType:
		// 	fmt.Println("Provisioning to Google Cloud Platform")
		// 	p := &google.Provisioner{}
		// 	err = p.Initialize(data)
		// 	if err != nil {
		// 		SetError(err, 6)
		// 		return
		// 	}

		// 	prov = p

		// case amazon.ProvisionerType:
		// 	fmt.Println("Provisioning to Amazon Web Services")
		// 	p := &amazon.Provisioner{}
		// 	err = p.Initialize(data)
		// 	if err != nil {
		// 		SetError(err, 7)
		// 		return
		// 	}

		// 	prov = p

		// case azure.ProvisionerType:
		// 	fmt.Println("Provisioning to Azure")
		// 	p := &azure.Provisioner{}
		// 	err = p.Initialize(data)
		// 	if err != nil {
		// 		SetError(err, 8)
		// 		return
		// 	}

		// 	prov = p
		// }

		buildablePath := "."
		if len(args) >= 1 {
			buildablePath = args[0]
		}

		pkgBuilder, err := getPackageBuilder("BUILDABLE", buildablePath)
		if err != nil {
			SetError(err, 9)

			return
		}

		err = modifyPackageBuilder(pkgBuilder)
		if err != nil {
			SetError(err, 10)
			return
		}

		pkgReader, err := vpkg.ReaderFromBuilder(pkgBuilder)
		if err != nil {
			SetError(err, 11)
			return
		}
		defer pkgReader.Close()

		pkgReader, err = vpkg.PeekVCFG(pkgReader)
		if err != nil {
			SetError(err, 12)
			return
		}

		err = initKernels()
		if err != nil {
			SetError(err, 13)
			return
		}

		f, err := ioutil.TempFile(os.TempDir(), "vorteil.disk")
		if err != nil {
			SetError(err, 14)
			return
		}
		defer os.Remove(f.Name())
		defer f.Close()

		err = vdisk.Build(context.Background(), f, &vdisk.BuildArgs{
			WithVCFGDefaults: true,
			PackageReader:    pkgReader,
			Format:           prov.DiskFormat(),
			SizeAlign:        int64(prov.SizeAlign()),
			KernelOptions: vdisk.KernelOptions{
				Shell: flagShell,
			},
			Logger: log,
		})
		if err != nil {
			SetError(err, 15)
			return
		}

		err = f.Close()
		if err != nil {
			SetError(err, 16)
			return
		}

		err = pkgReader.Close()
		if err != nil {
			SetError(err, 17)
			return
		}

		image, err := vio.LazyOpen(f.Name())
		if err != nil {
			SetError(err, 18)
			return
		}

		if provisionName == "" {
			provisionName = generateProvisionUUID()
			log.Infof("--name flag what not set using generated uuid '%s'", provisionName)
		}

		ctx := context.TODO()
		err = prov.Provision(&provisioners.ProvisionArgs{
			Context:         ctx,
			Image:           image,
			Name:            provisionName,
			Description:     provisionDescription,
			Force:           provisionForce,
			ReadyWhenUsable: provisionReadyWhenUsable,
		})
		if err != nil {
			SetError(err, 19)
			return
		}

		fmt.Printf("Finished creating image.\n")
	},
}

func generateProvisionUUID() string {
	pName := strings.ReplaceAll(uuid.New().String(), "-", "")

	// Replace first character with v if its a number
	if _, err := strconv.Atoi(pName[:1]); err == nil {
		pName = "v" + pName[1:]
	}

	return pName
}

var (
	provisionName            string
	provisionDescription     string
	provisionForce           bool
	provisionReadyWhenUsable bool
	provisionPassPhrase      string
)

func init() {
	f := provisionCmd.Flags()
	f.StringVarP(&provisionName, "name", "n", "", "Name of the resulting image on the remote platform.")
	f.StringVarP(&provisionDescription, "description", "D", "", "Description for the resulting image, if supported by the platform.")
	f.BoolVarP(&provisionForce, "force", "f", false, "Force an overwrite if an existing image conflicts with the new.")
	f.BoolVarP(&provisionReadyWhenUsable, "ready-when-usable", "r", false, "Return successfully as soon as the operation is complete, regardless of whether or not the platform is still processing the image.")
	f.StringVarP(&provisionPassPhrase, "passphrase", "s", "", "Passphrase used to decrypt encrypted provisioner data.")
}

var provisionersCmd = &cobra.Command{
	Use:     "provisioners",
	Short:   "Helper commands for working with Vorteil provisioners",
	Long:    ``,
	Example: ``,
}

var provisionersNewCmd = &cobra.Command{
	Use:   "new",
	Short: "Add a new provisioner.",
}

var (
	provisionersNewPassphrase string

	// Google Cloud Platform
	provisionersNewGoogleBucket  string
	provisionersNewGoogleKeyFile string

	// Amazon Web Services
	provisionersNewAmazonKey    string
	provisionersNewAmazonRegion string
	provisionersNewAmazonBucket string
	provisionersNewAmazonSecret string

	// Azure
	provisionersNewAzureContainer          string
	provisionersNewAzureKeyFile            string
	provisionersNewAzureLocation           string
	provisionersNewAzureResourceGroup      string
	provisionersNewAzureStorageAccountKey  string
	provisionersNewAzureStorageAccountName string
)

var provisionersNewAmazonEC2Cmd = &cobra.Command{
	Use:   "amazon-ec2 <OUTPUT_FILE>",
	Short: "Add a new AWS (Amazon Web Services) Provisioner.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		f, err := os.OpenFile(args[0], os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			SetError(err, 1)
			return
		}
		defer f.Close()

		p, err := amazon.NewProvisioner(log, &amazon.Config{
			Key:    provisionersNewAmazonKey,
			Secret: provisionersNewAmazonSecret,
			Region: provisionersNewAmazonRegion,
			Bucket: provisionersNewAmazonBucket,
		})
		if err != nil {
			SetError(err, 2)
			return
		}

		data, err := p.Marshal()
		if err != nil {
			SetError(err, 3)
			return
		}

		spew.Dump(data)

		out := provisioners.Encrypt(data, provisionersNewPassphrase)
		_, err = io.Copy(f, bytes.NewReader(out))
		if err != nil {
			SetError(err, 4)
			return
		}

	},
}

func init() {
	f := provisionersNewAmazonEC2Cmd.Flags()
	f.StringVarP(&provisionersNewAmazonKey, "key", "k", "", "Access key ID")
	provisionersNewAmazonEC2Cmd.MarkFlagRequired("key")
	f.StringVarP(&provisionersNewAmazonSecret, "secret", "s", "", "Secret access key")
	provisionersNewAmazonEC2Cmd.MarkFlagRequired("secret")
	f.StringVarP(&provisionersNewAmazonRegion, "region", "r", "ap-southeast-2", "AWS region")
	f.StringVarP(&provisionersNewAmazonBucket, "bucket", "b", "", "AWS bucket")
	f.StringVarP(&provisionersNewPassphrase, "passphrase", "p", "", "Passphrase for encrypting exported provisioner data.")
}

var provisionersNewAzureCmd = &cobra.Command{
	Use:   "azure <OUTPUT_FILE>",
	Short: "Add a new Microsoft Azure Provisioner.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		f, err := os.OpenFile(args[0], os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			SetError(err, 1)
			return
		}
		defer f.Close()

		path := provisionersNewAzureKeyFile
		_, err = os.Stat(path)
		if err != nil {
			SetError(err, 2)
			return
		}

		b, err := ioutil.ReadFile(path)
		if err != nil {
			SetError(err, 3)
			return
		}

		p, err := azure.NewProvisioner(log, &azure.Config{
			Key:                base64.StdEncoding.EncodeToString(b),
			Container:          provisionersNewAzureContainer,
			Location:           provisionersNewAzureLocation,
			ResourceGroup:      provisionersNewAzureResourceGroup,
			StorageAccountKey:  provisionersNewAzureStorageAccountKey,
			StorageAccountName: provisionersNewAzureStorageAccountName,
		})
		if err != nil {
			SetError(err, 4)
			return
		}

		data, err := p.Marshal()
		if err != nil {
			SetError(err, 5)
			return
		}

		out := provisioners.Encrypt(data, provisionersNewPassphrase)
		_, err = io.Copy(f, bytes.NewReader(out))
		if err != nil {
			SetError(err, 6)
			return
		}

	},
}

func init() {
	f := provisionersNewAzureCmd.Flags()
	f.StringVarP(&provisionersNewAzureKeyFile, "key-file", "k", "", "Azure 'Service Principal' credentials file")
	provisionersNewAzureCmd.MarkFlagRequired("key-file")
	f.StringVarP(&provisionersNewAzureContainer, "container", "c", "", "Azure container name")
	provisionersNewAzureCmd.MarkFlagRequired("container")
	f.StringVarP(&provisionersNewAzureResourceGroup, "resource-group", "r", "", "Azure resource group name")
	provisionersNewAzureCmd.MarkFlagRequired("resource-group")
	f.StringVarP(&provisionersNewAzureLocation, "location", "l", "", "Azure location")
	provisionersNewAzureCmd.MarkFlagRequired("location")
	f.StringVarP(&provisionersNewAzureStorageAccountKey, "storage-account-key", "s", "", "Azure storage account key")
	provisionersNewAzureCmd.MarkFlagRequired("storage-account-key")
	f.StringVarP(&provisionersNewAzureStorageAccountName, "storage-account-name", "n", "", "Azure storage account name")
	provisionersNewAzureCmd.MarkFlagRequired("storage-account-name")
	f.StringVarP(&provisionersNewPassphrase, "passphrase", "p", "", "Passphrase for encrypting exported provisioner data.")

}

var provisionersNewGoogleCmd = &cobra.Command{
	Use:   "google <OUTPUT_FILE>",
	Short: "Add a new Google Cloud (Compute Engine) Provisioner.",
	Args:  cobra.ExactArgs(1), // Single arg, points to output file
	Run: func(cmd *cobra.Command, args []string) {

		f, err := os.OpenFile(args[0], os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			SetError(err, 1)
			return
		}
		defer f.Close()

		path := provisionersNewGoogleKeyFile
		_, err = os.Stat(path)
		if err != nil {
			SetError(err, 2)
			return
		}

		b, err := ioutil.ReadFile(path)
		if err != nil {
			SetError(err, 3)
			return
		}

		p, err := google.NewProvisioner(log, &google.Config{
			Bucket: provisionersNewGoogleBucket,
			Key:    base64.StdEncoding.EncodeToString(b),
		})
		if err != nil {
			SetError(err, 4)
			return
		}

		data, err := p.Marshal()
		if err != nil {
			SetError(err, 5)
			return
		}

		out := provisioners.Encrypt(data, provisionersNewPassphrase)
		_, err = io.Copy(f, bytes.NewReader(out))
		if err != nil {
			SetError(err, 6)
			return
		}
	},
}

func init() {
	f := provisionersNewGoogleCmd.Flags()
	f.StringVarP(&provisionersNewPassphrase, "passphrase", "p", "", "Passphrase for encrypting exported provisioner data.")
	f.StringVarP(&provisionersNewGoogleBucket, "bucket", "b", "", "Name of an existing Google Cloud Storage bucket, for which the provided service account credentials have adequate permissions for object creation/deletion.")
	provisionersNewGoogleCmd.MarkFlagRequired("bucket")
	f.StringVarP(&provisionersNewGoogleKeyFile, "credentials", "f", "", "Path of an existing JSON-formatted Google Cloud Platform service account credentials file.")
	provisionersNewGoogleCmd.MarkFlagRequired("credentials")
}
