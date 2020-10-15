package amazon

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/google/uuid"
	"github.com/vorteil/vorteil/pkg/provisioners"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
)

// ProvisionerType : Constant string value used to represent the provisioner type amazon
const ProvisionerType = "amazon-ec2"

var ownerStrings = []*string{aws.String("830931392213")}
var ami = ""
var machineType = "t2.nano"
var provisionerID = "Amazon-EC2"

var pollrate = time.Millisecond * 1000
var securityGroupName = "vorteil-provisioner"
var securityGroupPort = int64(443)

// Provisioner satisfies the provisioners.Provisioner interface
type Provisioner struct {
	cfg *Config

	// aws
	ec2Client   *ec2.EC2
	s3Client    *s3.S3
	awsSession  *session.Session
	httpClient  *http.Client
	ec2UserData string
	args        provisioners.ProvisionArgs
}

// Config contains configuration fields required by the Provisioner
type Config struct {
	Key    string `json:"key"`    // AWS Access Key
	Secret string `json:"secret"` // AWS Access Key Secret
	Region string `json:"region"` // AWS Region
	Bucket string `json:"bucket"` // AWS Bucket
}

type userData struct {
	Reboot string `json:"SSDC_REBOOT"`
	Port   string `json:"SSDC_PORT"`
	Cert   string `json:"SSDC_CERT"`
	Key    string `json:"SSDC_KEY"`
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

// init / validate the provisioner
func (p *Provisioner) init() error {
	// Validate
	if p.cfg.Key == "" {
		return errors.New("no defined access key")
	}

	if p.cfg.Secret == "" {
		return errors.New("no defined access secret")
	}

	if p.cfg.Region == "" {
		return errors.New("no defined region")
	}

	if p.cfg.Bucket == "" {
		return errors.New("no defined bucket")
	}

	// attempt to connect and validate that the provided config is workable
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(p.cfg.Region),
		Credentials: credentials.NewStaticCredentials(p.cfg.Key, p.cfg.Secret, ""),
	})
	if err != nil {
		return err
	}

	region, err := s3manager.GetBucketRegion(context.Background(), sess, p.cfg.Bucket, p.cfg.Region)
	if err != nil {
		return fmt.Errorf("bucket '%s' does not exist", p.cfg.Bucket)
	}

	if region != p.cfg.Region {
		return fmt.Errorf("bucket '%s' does not exist in region '%s'", p.cfg.Bucket, p.cfg.Region)
	}

	return nil
}

// Type returns 'amazon-ec2'
func (p *Provisioner) Type() string {
	return ProvisionerType
}

// DiskFormat returns the provisioners required disk format
func (p *Provisioner) DiskFormat() vdisk.Format {
	return vdisk.VHDDynamicFormat
}

// SizeAlign returns vcfg GiB size in bytes
func (p *Provisioner) SizeAlign() vcfg.Bytes {
	return vcfg.GiB
}

// Initialize ..
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

	p.awsSession, err = session.NewSession(&aws.Config{
		Region:      aws.String(p.cfg.Region),
		Credentials: credentials.NewStaticCredentials(p.cfg.Key, p.cfg.Secret, ""),
	})

	if err != nil {
		return err
	}

	p.s3Client = s3.New(p.awsSession)
	p.ec2Client = ec2.New(p.awsSession, aws.NewConfig().WithRegion(p.cfg.Region))

	return nil
}

// Provision given a valid ProvisionArgs object will provision the passed vorteil project
//	to the configured amazon provisioner. This process will return as soon as the vorteil
//	projects image has been uploaded, unless ReadyWhenUsable was set to true, then
//	this function will block until aws reports the ami as usable.
func (p *Provisioner) Provision(args *provisioners.ProvisionArgs) error {
	var err error
	var imageID *string
	p.args = *args

	uploadProgress := args.Logger.NewProgress("Uploading Image to AWS Bucket", "", 0)
	defer uploadProgress.Finish(true)

	// Handle Exisitng Image and Force Flag
	imageID, err = p.getImageID(p.args.Name)
	if imageID != nil {
		if args.Force {
			// deregister current live version as were force pushing
			p.args.Logger.Infof("deregistering old ami: %v\n", imageID)
			_, err = p.ec2Client.DeregisterImageWithContext(p.args.Context, &ec2.DeregisterImageInput{
				ImageId: imageID,
			})
		} else {
			err = errors.New("ami exists: try using the --force flag")
		}

	}

	if err != nil {
		return err
	}

	// Upload Image
	keyName := aws.String(p.args.Name + "-" + uuid.New().String())
	uploader := s3manager.NewUploader(p.awsSession)
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(p.cfg.Bucket),
		Key:    keyName,
		Body:   args.Image,
	})

	uploadProgress.Finish(true)

	if err != nil {
		return fmt.Errorf("Failed to upload image to bucket '%s', error: %s", p.cfg.Bucket, err.Error())
	}

	defer func() {
		p.args.Logger.Infof("Cleaning Image From Bucket %s", keyName)
		// Delete object that was uploaded (mainly used to clean up when the function ends)
		_, _ = p.s3Client.DeleteObject(&s3.DeleteObjectInput{
			Bucket: aws.String(p.cfg.Bucket),
			Key:    keyName,
		})
	}()

	snapshotID, err := p.importSnapshot(aws.StringValue(keyName))
	if err != nil {
		return fmt.Errorf("Failed to convert bucket Image to Snapshot, error: %s", err.Error())
	}

	registerImgProgress := p.args.Logger.NewProgress("Registering snapshot as AMI", "", 0)
	defer registerImgProgress.Finish(true)
	rio, err := p.ec2Client.RegisterImage(&ec2.RegisterImageInput{
		Architecture:       aws.String("x86_64"),
		Description:        aws.String(p.args.Description),
		Name:               aws.String(p.args.Name),
		EnaSupport:         aws.Bool(true),
		VirtualizationType: aws.String("hvm"),
		RootDeviceName:     aws.String("/dev/sda1"),
		BlockDeviceMappings: []*ec2.BlockDeviceMapping{
			&ec2.BlockDeviceMapping{
				DeviceName: aws.String("/dev/sda1"),
				Ebs: &ec2.EbsBlockDevice{
					SnapshotId: aws.String(snapshotID),
				},
			},
		},
	})
	if err != nil {
		return err
	}
	registerImgProgress.Finish(true)

	args.Logger.Printf("Provisioned AMI: %s", *rio.ImageId)
	return nil
}

// getImageID given a imageName, return the imageID of the first image found, or nil if not found
func (p *Provisioner) getImageID(imageName string) (*string, error) {
	var err error
	filterForce := &ec2.Filter{
		Name:   aws.String("name"),
		Values: []*string{aws.String(imageName)},
	}
	awsImages, err := p.ec2Client.DescribeImages(&ec2.DescribeImagesInput{
		Filters: []*ec2.Filter{filterForce},
	})
	if err != nil {
		return nil, fmt.Errorf("Could not get image ID for image '%s', error: %v", imageName, err)
	}

	if len(awsImages.Images) > 0 {
		return awsImages.Images[0].ImageId, nil
	}

	return nil, nil
}

func (p *Provisioner) importSnapshot(bucketImageKey string) (string, error) {
	snapshotProgress := p.args.Logger.NewProgress("Converting Image to Snapshot ", "%", 100)
	defer snapshotProgress.Finish(false)
	// Import Snapshot
	var snapshotID *string
	// o.updateStatus("Importing disk into EBS Snapshot")
	iso, err := p.ec2Client.ImportSnapshot(&ec2.ImportSnapshotInput{
		Description: aws.String(p.args.Description),
		DiskContainer: &ec2.SnapshotDiskContainer{
			UserBucket: &ec2.UserBucket{
				S3Bucket: aws.String(p.cfg.Bucket),
				S3Key:    aws.String(bucketImageKey),
			},
			Format: aws.String("VHD"),
		},
	})
	if err != nil {
		return aws.StringValue(snapshotID), err
	}

	var progress int = 0
	var disto *ec2.DescribeImportSnapshotTasksOutput
	for {
		disto, err = p.ec2Client.DescribeImportSnapshotTasks(&ec2.DescribeImportSnapshotTasksInput{
			ImportTaskIds: []*string{iso.ImportTaskId},
		})
		if err != nil {
			break
		}

		// Check if task exists
		if len(disto.ImportSnapshotTasks) > 0 {
			if disto.ImportSnapshotTasks[0].SnapshotTaskDetail.StatusMessage != nil && disto.ImportSnapshotTasks[0].SnapshotTaskDetail.Progress != nil {
				progressIncrement := 0
				newProgress, _ := strconv.Atoi(*disto.ImportSnapshotTasks[0].SnapshotTaskDetail.Progress)
				if newProgress != 0 {
					progressIncrement = newProgress - progress
					progress += progressIncrement
				}
				snapshotProgress.Increment(int64(progressIncrement))
			}

			if *disto.ImportSnapshotTasks[0].SnapshotTaskDetail.Status == *aws.String("completed") {
				progress = 100
				snapshotID = disto.ImportSnapshotTasks[0].SnapshotTaskDetail.SnapshotId
				break
			}
			// Task errored out hence deleting return status message as error
			if disto.ImportSnapshotTasks[0].SnapshotTaskDetail.Status == aws.String("deleted") || disto.ImportSnapshotTasks[0].SnapshotTaskDetail.Status == aws.String("deleting") {
				err = errors.New(*disto.ImportSnapshotTasks[0].SnapshotTaskDetail.StatusMessage)
				break
			}
		} else {
			err = errors.New("No import id tasks exists for the snapshot")
			break
		}
		time.Sleep(pollrate)
	}
	if progress == 100 {
		snapshotProgress.Finish(true)
	} else {
		snapshotProgress.Finish(false)
	}

	return aws.StringValue(snapshotID), err
}

// Marshal returns json provisioner as bytes
func (p *Provisioner) Marshal() ([]byte, error) {
	m := make(map[string]interface{})
	m[provisioners.MapKey] = ProvisionerType
	m["key"] = p.cfg.Key
	m["secret"] = p.cfg.Secret
	m["region"] = p.cfg.Region
	m["bucket"] = p.cfg.Bucket

	out, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	return out, nil
}
