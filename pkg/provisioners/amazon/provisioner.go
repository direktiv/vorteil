/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package amazon

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

	provisionProgress := p.args.Logger.NewProgress("Provising to AWS", "", 0)
	defer provisionProgress.Finish(true)

	// Create EC2 Client
	p.client, p.httpClient, p.ec2UserData, err = p.createClient()
	if err != nil {
		return err
	}

	instanceID, err := p.createEmptyInstance()
	if err != nil {
		return err
	}

	defer func() {
		var err error
		args.Logger.Infof("Instance status: Attempting to terminate instance...")
		_, err = p.client.TerminateInstances(&ec2.TerminateInstancesInput{
			InstanceIds: []*string{
				&instanceID,
			},
		})
		if err != nil {
			args.Logger.Infof("An error occurred trying to clean up instance %s: %v.\n", instanceID, err)
		}
		args.Logger.Infof("Instance status: terminated.")
	}()

	ip, err := p.waitForInstanceToBeReady(instanceID)
	if err != nil {
		return err
	}

	err = p.uploadedPayloadToInstance(ip)
	if err != nil {
		return err
	}

	ami, err = p.pushAMI(instanceID)
	if err != nil {
		return err
	}

	p.args.Logger.Printf("Successfully Provisioned ami '%s'", ami)

	return nil
}

func (p *Provisioner) createClient() (*ec2.EC2, *http.Client, string, error) {
	var err error

	// Create EC2 Client
	p.args.Logger.Infof("Creating new session...")
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(p.cfg.Region),
		Credentials: credentials.NewStaticCredentials(p.cfg.Key, p.cfg.Secret, ""),
	})
	if err != nil {
		return nil, nil, "", err
	}

	p.args.Logger.Infof("Session created.")

	p.args.Logger.Infof("Creating new client...")
	ec2Client := ec2.New(sess, aws.NewConfig().WithRegion(p.cfg.Region))
	p.args.Logger.Infof("Client created.")

	// Create EC2 Single Cert Data
	cert, key := generateCertificate()
	p.args.Logger.Infof("Single-use certificates created.")

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

func (p *Provisioner) createEmptyInstance() (string, error) {
	var instanceID string
	var err error

	// if not being forced up check if it exists
	if !p.args.Force {

		filterForce := &ec2.Filter{
			Name:   aws.String("name"),
			Values: []*string{aws.String(p.args.Name)},
		}
		var imagesForce *ec2.DescribeImagesOutput
		imagesForce, err = p.client.DescribeImages(&ec2.DescribeImagesInput{
			Filters: []*ec2.Filter{filterForce},
		})
		if err != nil {
			return instanceID, err
		}
		if len(imagesForce.Images) > 0 {
			return instanceID, errors.New("ami exists: try using the --force flag")
		}
	}
	p.args.Logger.Infof("Looking up security group ID...")

	secgrps, err := p.client.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupNames: []*string{aws.String(securityGroupName)},
	})
	if err != nil {
		return instanceID, err
	}

	var hasAccess bool
	for _, perm := range secgrps.SecurityGroups[0].IpPermissions {
		if (perm.FromPort != nil && *perm.FromPort == securityGroupPort) &&
			(perm.ToPort != nil && *perm.ToPort == securityGroupPort) &&
			(perm.IpProtocol != nil && *perm.IpProtocol == "tcp") {
			hasAccess = true
		}
	}

	if !hasAccess {
		return instanceID, fmt.Errorf("the %s security group must allow TCP ingress on port %d", securityGroupName, securityGroupPort)
	}

	securityGroupID := *secgrps.SecurityGroups[0].GroupId
	p.args.Logger.Infof("Security group: %s\n", securityGroupID)
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

	p.args.Logger.Infof("Disk size: %d GiB\n", gigs.Units(vcfg.GiB))
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
	if err != nil {
		return instanceID, err
	}
	instanceID = *reservation.Instances[0].InstanceId

	p.args.Logger.Infof("Created empty instance: %s.\n", instanceID)

	return instanceID, nil
}

func ec2PublicIPReady(description *ec2.DescribeInstancesOutput) (ip string, ready bool) {
	if description == nil || len(description.Reservations) == 0 ||
		len(description.Reservations[0].Instances) == 0 ||
		len(description.Reservations[0].Instances[0].NetworkInterfaces) == 0 ||
		description.Reservations[0].Instances[0].NetworkInterfaces[0].Association == nil ||
		description.Reservations[0].Instances[0].NetworkInterfaces[0].Association.PublicIp == nil {
		return
	}

	ip = *description.Reservations[0].Instances[0].NetworkInterfaces[0].Association.PublicIp
	if ip != "" {
		ready = true
	}
	return
}

func (p *Provisioner) waitForInstanceToBeReady(instanceID string) (string, error) {
	var ip string
	var ipReady bool
	for {
		time.Sleep(pollrate)
		description, err := p.client.DescribeInstancesWithContext(p.args.Context, &ec2.DescribeInstancesInput{
			InstanceIds: []*string{
				&instanceID,
			},
		})
		if err != nil {
			return ip, err
		}

		if description == nil || len(description.Reservations) == 0 ||
			len(description.Reservations[0].Instances) == 0 ||
			description.Reservations[0].Instances[0].State == nil {
			continue
		}
		switch *description.Reservations[0].Instances[0].State.Code & 0xFF {
		case 0:
			p.args.Logger.Infof("Instance status: pending.")
			continue
		case 16:
			p.args.Logger.Infof("Instance status: running.")
			ip, ipReady = ec2PublicIPReady(description)
			if !ipReady {
				continue
			}
		case 32:
			p.args.Logger.Infof("Instance status: shutting-down.")
			return ip, errors.New("instance is shutting down for an unknown reason")
		case 48:
			p.args.Logger.Infof("Instance status: terminated.")
			return ip, errors.New("instance has been terminated for an unknown reason")
		case 64:
			p.args.Logger.Infof("Instance status: stopping.")
			return ip, errors.New("instance is stopping for an unknown reason")
		case 80:
			p.args.Logger.Infof("Instance status: stopped.")
			return ip, errors.New("instance stopped for an unknown reason")
		default:
			p.args.Logger.Infof("Instance status: unknown.")
			continue
		}

		break
	}
	p.args.Logger.Infof("Instance public IP address: %s\n", ip)

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("https://%s:%d/", ip, securityGroupPort), nil)
	if err != nil {
		return ip, err
	}
	req = req.WithContext(p.args.Context)
	max := 6
	tries := 0
	gap := time.Second
	for {
		tries++
		p.args.Logger.Infof("Polling instance for connectivity (%d/%d)...\n", tries, max)
		ctx, cancel := context.WithTimeout(p.args.Context, time.Second*10)
		req = req.WithContext(ctx)
		resp, err := p.httpClient.Do(req)
		cancel()
		if err != nil {
			p.args.Logger.Infof("Trying %v out of %v\n", tries, max)
			p.args.Logger.Infof("Error on POST: %v\n", err)
			if tries == max {
				return ip, errors.New("instance failed to respond")
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

	p.args.Logger.Infof("Instance is live and ready for payload.")
	return ip, nil
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

	p.args.Logger.Infof("Payload dispatched.")
	checksum := hex.EncodeToString(hasher.Sum(nil))
	p.args.Logger.Infof("Our checksum: %s\n", checksum)

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("server error [%d]: %s", resp.StatusCode, data)
	}

	p.args.Logger.Infof("Server checksum: %s\n", data)
	return nil
}

func (p *Provisioner) pushAMI(instanceID string) (string, error) {
	for {
		time.Sleep(pollrate)
		description, err := p.client.DescribeInstancesWithContext(p.args.Context, &ec2.DescribeInstancesInput{
			InstanceIds: []*string{
				&instanceID,
			},
		})
		if err != nil {
			return "", err
		}

		if description == nil || len(description.Reservations) == 0 ||
			len(description.Reservations[0].Instances) == 0 ||
			description.Reservations[0].Instances[0].State == nil {
			continue
		}
		switch *description.Reservations[0].Instances[0].State.Code & 0xFF {
		case 0:
			p.args.Logger.Infof("Instance status: pending.")
			return "", errors.New("instance is restarting for an unknown reason")
		case 16:
			p.args.Logger.Infof("Instance status: running.")
			continue
		case 32:
			p.args.Logger.Infof("Instance status: shutting-down.")
			continue
		case 48:
			p.args.Logger.Infof("Instance status: terminated.")
			return "", errors.New("instance has been terminated for an unknown reason")
		case 64:
			p.args.Logger.Infof("Instance status: stopping.")
			continue
		case 80:
			p.args.Logger.Infof("Instance status: stopped.")
		default:
			p.args.Logger.Infof("Instance status: unknown.")
			continue
		}

		break
	}

	p.args.Logger.Infof("Instance has stopped.")

	// make AMI
	// if args.Force, check if ami exists with same name
	if p.args.Force {
		filterForce := &ec2.Filter{
			Name:   aws.String("name"),
			Values: []*string{aws.String(p.args.Name)},
		}
		imagesForce, err := p.client.DescribeImages(&ec2.DescribeImagesInput{
			Filters: []*ec2.Filter{filterForce},
		})
		if err != nil {
			return "", err
		}
		if len(imagesForce.Images) > 0 {
			// deregister current live version as were force pushing
			p.args.Logger.Infof("deregistering old ami: %v\n", imagesForce.Images[0].ImageId)
			_, err := p.client.DeregisterImageWithContext(p.args.Context, &ec2.DeregisterImageInput{
				ImageId: imagesForce.Images[0].ImageId,
			})
			if err != nil {
				return "", err
			}
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

	p.args.Logger.Infof("AMI ID: %s\n", *img.ImageId)

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
			p.args.Logger.Infof("Image status: %s.\n", *description.Images[0].State)
			if *description.Images[0].State == "available" {
				break
			}
		}
	}

	p.args.Logger.Printf("Provisioned AMI: %s\n", *img.ImageId)
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
