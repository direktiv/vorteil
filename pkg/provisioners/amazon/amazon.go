package amazon

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/vorteil/vorteil/pkg/elog"
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
	log elog.View

	// aws
	client      *ec2.EC2
	httpClient  *http.Client
	ec2UserData string
	args        provisioners.ProvisionArgs
}

// Config contains configuration fields required by the Provisioner
type Config struct {
	Key    string `json:"key"`    // AWS Access Key
	Secret string `json:"secret"` // AWS Access Key Secret
	Region string `json:"region"` // AWS Region
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

func (p *Provisioner) init() error {

	return nil
}

// Type returns 'amazon-ec2'
func (p *Provisioner) Type() string {
	return ProvisionerType
}

// DiskFormat returns the provisioners required disk format
func (p *Provisioner) DiskFormat() vdisk.Format {
	return vdisk.RAWFormat
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

	return nil
}

// Provision given a valid ProvisionArgs object will provision the passed vorteil project
//	to the configured amazon provisioner. This process will return as soon as the vorteil
//	projects image has been uploaded, unless ReadyWhenUsable was set to true, then
//	this function will block until aws reports the ami as usable.
func (p *Provisioner) Provision(args *provisioners.ProvisionArgs) error {
	p.args = *args
	var err error
	var instanceID string
	var instanceIP string

	provisionProgress := p.log.NewProgress("Provisioning to AWS", "", 0)
	defer provisionProgress.Finish(true)

	// Create EC2 Client
	p.client, p.httpClient, p.ec2UserData, err = p.createClient()
	if err != nil {
		p.log.Errorf("failed to create client or ec2 userdata for aws")
		return err
	}

	instanceID, err = p.createEmptyInstance()
	if err != nil {
		p.log.Errorf("failed to create empty instance")
		return err
	}
	p.log.Infof("Created empty instance: %s.\n", instanceID)

	defer func() {
		var err error
		p.log.Infof("Instance status: Attempting to terminate instance...")
		_, err = p.client.TerminateInstances(&ec2.TerminateInstancesInput{
			InstanceIds: []*string{
				&instanceID,
			},
		})
		if err != nil {
			p.log.Infof("An error occurred trying to clean up instance %s: %v.\n", instanceID, err)
		}
		p.log.Infof("Instance status: terminated.")
	}()

	instanceIP, err = p.getInstancePublicIP(instanceID)
	if err != nil {
		p.log.Errorf("failed to get live instances public ip address")
		return err
	}

	p.log.Infof("Instance public IP address: %s\n", instanceIP)

	err = p.prepareInstaceForPayload(instanceIP, instanceID)
	if err != nil {
		p.log.Errorf("failed to prepare instance for raw image payload")
		return err
	}

	p.log.Infof("Instance is live and ready for payload.")

	err = p.uploadedPayloadToInstance(instanceIP)
	if err != nil {
		p.log.Errorf(fmt.Sprintf("failed to upload raw image payload to instance '%s'", instanceID))
		return err
	}

	ami, err = p.pushAMI(instanceID)
	if err != nil {
		p.log.Errorf(fmt.Sprintf("failed to create ami from instance '%s'", instanceID))
	} else {
		p.log.Printf("Successfully Provisioned ami '%s'", ami)
	}
	return err
}

func (p *Provisioner) createClient() (*ec2.EC2, *http.Client, string, error) {
	var err error

	// Create EC2 Client
	p.log.Infof("Creating new session...")
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(p.cfg.Region),
		Credentials: credentials.NewStaticCredentials(p.cfg.Key, p.cfg.Secret, ""),
	})
	if err != nil {
		return nil, nil, "", err
	}

	p.log.Infof("Session created.")

	p.log.Infof("Creating new client...")
	ec2Client := ec2.New(sess, aws.NewConfig().WithRegion(p.cfg.Region))
	p.log.Infof("Client created.")

	// Create EC2 Single Cert Data
	cert, key := generateCertificate()
	p.log.Infof("Single-use certificates created.")

	data, err := json.Marshal(&userData{
		Reboot: "false",
		Port:   fmt.Sprintf("%d", int(securityGroupPort)),
		Cert:   string(cert),
		Key:    string(key),
	})
	if err != nil {
		return nil, nil, "", err
	}
	userdata := base64.StdEncoding.EncodeToString(data)

	// Create http client
	tlsCert, err := tls.X509KeyPair(cert, key)
	if err != nil {
		panic(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(cert)

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{tlsCert},
		RootCAs:            caCertPool,
		InsecureSkipVerify: true,
	}
	tlsConfig.BuildNameToCertificate()
	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return ec2Client, httpClient, userdata, nil
}

func (p *Provisioner) forceOverwriteCheck() error {
	if !p.args.Force {
		filterForce := &ec2.Filter{
			Name:   aws.String("name"),
			Values: []*string{aws.String(p.args.Name)},
		}
		var imagesForce *ec2.DescribeImagesOutput
		imagesForce, err := p.client.DescribeImages(&ec2.DescribeImagesInput{
			Filters: []*ec2.Filter{filterForce},
		})
		if err != nil {
			return err
		}
		if len(imagesForce.Images) > 0 {
			return errors.New("ami exists: try using the --force flag")
		}
	}

	return nil
}

func checkSecurityGroupAccess(securityGroup *ec2.SecurityGroup) bool {
	for _, perm := range securityGroup.IpPermissions {
		// Check if permissions are nil
		if perm.FromPort == nil || perm.ToPort == nil || perm.IpProtocol == nil {
			continue
		}

		// Check if permissions are valid
		if *perm.FromPort == securityGroupPort &&
			*perm.ToPort == securityGroupPort &&
			*perm.IpProtocol == "tcp" {
			return true
		}
	}

	return false
}

func (p *Provisioner) getSecurityGroups() (*ec2.DescribeSecurityGroupsOutput, error) {
	secGroups, err := p.client.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupNames: []*string{aws.String(securityGroupName)},
	})
	if err != nil {
		return nil, err
	}

	if !checkSecurityGroupAccess(secGroups.SecurityGroups[0]) {
		return nil, fmt.Errorf("the %s security group must allow TCP ingress on port %d", securityGroupName, securityGroupPort)
	}

	return secGroups, nil
}

func (p *Provisioner) createEmptyInstance() (string, error) {
	var instanceID string

	// Check force flag: if force is false and ami exists return error
	if forceErr := p.forceOverwriteCheck(); forceErr != nil {
	}

	p.log.Infof("Looking up security group ID...")
	secGroups, err := p.getSecurityGroups()
	if err != nil {
		return instanceID, err
	}

	securityGroupID := *secGroups.SecurityGroups[0].GroupId
	p.log.Infof("Security group: %s\n", securityGroupID)
	filter := &ec2.Filter{
		Name:   aws.String("name"),
		Values: []*string{aws.String("vorteil-compiler")},
	}
	images, err := p.client.DescribeImages(&ec2.DescribeImagesInput{
		Owners:  ownerStrings,
		Filters: []*ec2.Filter{filter},
	})
	if err != nil {
		return instanceID, err
	}

	gigs := vcfg.Bytes(p.args.Image.Size())
	gigs.Align(vcfg.GiB)

	p.log.Infof("Disk size: %d GiB\n", gigs.Units(vcfg.GiB))
	ami = *images.Images[0].ImageId
	reservation, err := p.client.RunInstancesWithContext(p.args.Context, &ec2.RunInstancesInput{
		MaxCount:         aws.Int64(1),
		MinCount:         aws.Int64(1),
		ImageId:          aws.String(ami),
		InstanceType:     aws.String(machineType),
		UserData:         &p.ec2UserData,
		SecurityGroupIds: []*string{&securityGroupID},
		BlockDeviceMappings: []*ec2.BlockDeviceMapping{
			{
				DeviceName: aws.String("/dev/sda1"),
				Ebs: &ec2.EbsBlockDevice{
					DeleteOnTermination: aws.Bool(true),
					VolumeSize:          aws.Int64(int64(gigs.Units(vcfg.GiB))),
				},
			},
		},
		// TODO: resize disk
	})
	if err == nil {
		instanceID = *reservation.Instances[0].InstanceId
	}
	return instanceID, err
}

func ec2PublicIPReady(description *ec2.DescribeInstancesOutput) (ip string, ready bool) {
	// Check if children exist
	if description == nil || len(description.Reservations) == 0 ||
		len(description.Reservations[0].Instances) == 0 ||
		len(description.Reservations[0].Instances[0].NetworkInterfaces) == 0 {
		return
	}

	// Check if publicIP exists
	if description.Reservations[0].Instances[0].NetworkInterfaces[0].Association == nil ||
		description.Reservations[0].Instances[0].NetworkInterfaces[0].Association.PublicIp == nil {
		return
	}

	ip = *description.Reservations[0].Instances[0].NetworkInterfaces[0].Association.PublicIp
	if ip != "" {
		ready = true
	}
	return
}

// getInstancePublicIP : Get Public Ip of Instance; Instance must be running or pending
func (p *Provisioner) getInstancePublicIP(instanceID string) (string, error) {
	var ip string
	var ipReady bool
	var err error
	for err == nil {
		time.Sleep(pollrate)
		description, descErr := p.client.DescribeInstancesWithContext(p.args.Context, &ec2.DescribeInstancesInput{
			InstanceIds: []*string{
				&instanceID,
			},
		})
		if err != nil {
			err = descErr
			break
		}

		if description == nil || len(description.Reservations) == 0 ||
			len(description.Reservations[0].Instances) == 0 ||
			description.Reservations[0].Instances[0].State == nil {
			continue
		}
		switch *description.Reservations[0].Instances[0].State.Code & 0xFF {
		case 0:
			p.log.Infof("Instance status: pending.")
			continue
		case 16:
			p.log.Infof("Instance status: running.")
			ip, ipReady = ec2PublicIPReady(description)
			if ipReady {
				return ip, err
			}
			continue
		case 32:
			p.log.Infof("Instance status: shutting-down.")
			err = errors.New("instance is shutting down for an unknown reason")
		case 48:
			p.log.Infof("Instance status: terminated.")
			err = errors.New("instance has been terminated for an unknown reason")
		case 64:
			p.log.Infof("Instance status: stopping.")
			err = errors.New("instance is stopping for an unknown reason")
		case 80:
			p.log.Infof("Instance status: stopped.")
			err = errors.New("instance stopped for an unknown reason")
		default:
			p.log.Infof("Instance status: unknown.")
			continue
		}
	}

	return ip, err
}

// prepareInstaceForPayload : Waits for instance ip to be ready for payload
func (p *Provisioner) prepareInstaceForPayload(ip, instanceID string) error {
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("https://%s:%d/", ip, securityGroupPort), nil)
	if err != nil {
		return err
	}
	req = req.WithContext(p.args.Context)
	max := 6
	tries := 0
	gap := time.Second
	for {
		tries++
		p.log.Infof("Polling instance for connectivity (%d/%d)...\n", tries, max)
		ctx, cancel := context.WithTimeout(p.args.Context, time.Second*10)
		req = req.WithContext(ctx)
		resp, err := p.httpClient.Do(req)
		cancel()
		if err != nil {
			p.log.Infof("Trying %v out of %v\n", tries, max)
			p.log.Infof("Error on POST: %v\n", err)
			if tries == max {
				return errors.New("instance failed to respond")
			}
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if err == nil {
			break
		}
		select {
		case <-p.args.Context.Done():
			// ?
		case <-time.After(gap):
		}
		gap *= 2
	}

	return nil
}

func (p *Provisioner) uploadedPayloadToInstance(ip string) error {
	gigs := vcfg.Bytes(p.args.Image.Size())
	gigs.Align(vcfg.GiB)
	pr, pw := io.Pipe()
	defer pr.Close()
	gz := gzip.NewWriter(pw)
	hasher := md5.New()
	mw := io.MultiWriter(gz, hasher)
	// counter := datacounter.NewWriterCounter(mw)
	go func() {
		// _, _ = io.Copy(counter, p.args.Image)
		_, _ = io.Copy(mw, p.args.Image)
		gz.Close()
		pw.Close()
	}()

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("https://%s:%d/", ip, securityGroupPort), pr)
	if err != nil {
		return err
	}

	// TODO: use server API to track disk write progress
	req.Header.Set("Disk-Size", fmt.Sprintf("%d", gigs.Units(1)))
	req = req.WithContext(p.args.Context)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("error posting RAW image to instance: %v", err)
	}
	defer resp.Body.Close()

	p.log.Infof("Payload dispatched.")
	checksum := hex.EncodeToString(hasher.Sum(nil))
	p.log.Infof("Our checksum: %s\n", checksum)

	var responseError error
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		responseError = fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != 200 {
		responseError = fmt.Errorf("server error [%d]: %s", resp.StatusCode, data)
	}

	if responseError != nil {
		return responseError
	}

	p.log.Infof("Server checksum: %s\n", data)
	return nil
}

func (p *Provisioner) waitForInstanceToStop(instanceID string) error {
	for {
		time.Sleep(pollrate)
		description, err := p.client.DescribeInstancesWithContext(p.args.Context, &ec2.DescribeInstancesInput{
			InstanceIds: []*string{
				&instanceID,
			},
		})
		if err != nil {
			return err
		}

		if description == nil || len(description.Reservations) == 0 ||
			len(description.Reservations[0].Instances) == 0 ||
			description.Reservations[0].Instances[0].State == nil {
			continue
		}
		switch *description.Reservations[0].Instances[0].State.Code & 0xFF {
		case 0:
			p.log.Infof("Instance status: pending.")
			return errors.New("instance is restarting for an unknown reason")
		case 16:
			p.log.Infof("Instance status: running.")
			continue
		case 32:
			p.log.Infof("Instance status: shutting-down.")
			continue
		case 48:
			p.log.Infof("Instance status: terminated.")
			return errors.New("instance has been terminated for an unknown reason")
		case 64:
			p.log.Infof("Instance status: stopping.")
			continue
		case 80:
			p.log.Infof("Instance status: stopped.")
		default:
			p.log.Infof("Instance status: unknown.")
			continue
		}

		break
	}

	return nil
}

// deregisterAMI : If ami with same name is found, deregister it
func (p *Provisioner) deregisterAMI() error {
	filterForce := &ec2.Filter{
		Name:   aws.String("name"),
		Values: []*string{aws.String(p.args.Name)},
	}
	imagesForce, err := p.client.DescribeImages(&ec2.DescribeImagesInput{
		Filters: []*ec2.Filter{filterForce},
	})
	if err != nil {
		return err
	}
	if len(imagesForce.Images) > 0 {
		// deregister current live version as were force pushing
		p.log.Infof("deregistering old ami: %v\n", imagesForce.Images[0].ImageId)
		_, err := p.client.DeregisterImageWithContext(p.args.Context, &ec2.DeregisterImageInput{
			ImageId: imagesForce.Images[0].ImageId,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *Provisioner) pushAMI(instanceID string) (string, error) {
	if err := p.waitForInstanceToStop(instanceID); err != nil {
		return "", err
	}
	p.log.Infof("Instance has stopped.")

	if p.args.Force {
		// attemp to deregister AMI
		if err := p.deregisterAMI(); err != nil {
			return "", err
		}
	}

	img, err := p.client.CreateImageWithContext(p.args.Context, &ec2.CreateImageInput{
		Description: aws.String(p.args.Description),
		InstanceId:  aws.String(instanceID),
		Name:        aws.String(p.args.Name),
		// TODO: Block Device Mappings?
	})
	if err != nil {
		return "", err
	}

	p.log.Infof("AMI ID: %s\n", *img.ImageId)

	if p.args.ReadyWhenUsable {
		for {
			time.Sleep(pollrate)
			description, err := p.client.DescribeImagesWithContext(p.args.Context, &ec2.DescribeImagesInput{
				ImageIds: []*string{
					img.ImageId,
				},
			})
			if err != nil {
				return "", err
			}

			if description == nil || len(description.Images) == 0 {
				continue
			}
			p.log.Infof("Image status: %s.\n", *description.Images[0].State)
			if *description.Images[0].State == "available" {
				break
			}
		}
	}

	p.log.Printf("Provisioned AMI: %s\n", *img.ImageId)
	return *img.ImageId, nil
}

// Marshal returns json provisioner as bytes
func (p *Provisioner) Marshal() ([]byte, error) {
	m := make(map[string]interface{})
	m[provisioners.MapKey] = ProvisionerType
	m["key"] = p.cfg.Key
	m["secret"] = p.cfg.Secret
	m["region"] = p.cfg.Region

	out, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	return out, nil
}

func generateCertificate() (cert []byte, key []byte) {

	serialno, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		panic(err)
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	certificate := x509.Certificate{
		SerialNumber: serialno,
		Subject: pkix.Name{
			Organization: []string{"Vorteil.io Pty Ltd"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Minute * 30),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		EmailAddresses:        []string{"support@vorteil.io"},
		IsCA:                  true,
	}

	data, err := x509.CreateCertificate(rand.Reader, &certificate, &certificate, &priv.PublicKey, priv)
	if err != nil {
		panic(err)
	}

	cert = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: data})
	key = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	return cert, key
}
