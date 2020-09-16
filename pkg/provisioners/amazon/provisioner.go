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

const ProvisionerType = "amazon-ec2"

var OwnerStrings = []*string{aws.String("830931392213")}

var AMI = ""
var MachineType = "t2.nano"
var ProvisionerID = "Amazon-EC2"

var pollrate = time.Millisecond * 1000
var securityGroupName = "vorteil-provisioner"
var securityGroupPort = int64(443)

// Provisioner satisfies the provisioners.Provisioner interface
type Provisioner struct {
	cfg *Config
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

func (p *Provisioner) Provision(args *provisioners.ProvisionArgs) error {

	var err error

	fmt.Println("Creating new session...")
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(p.cfg.Region),
		Credentials: credentials.NewStaticCredentials(p.cfg.Key, p.cfg.Secret, ""),
	})
	if err != nil {
		return err
	}
	fmt.Println("Session created.")

	fmt.Println("Creating new client...")
	client := ec2.New(sess, aws.NewConfig().WithRegion(p.cfg.Region))
	fmt.Println("Client created.")

	fmt.Println("Generating instance metadata...")
	cert, key := generateCertificate()
	fmt.Println("Single-use certificates created.")

	data, err := json.Marshal(&userData{
		Reboot: "false",
		Port:   fmt.Sprintf("%d", int(securityGroupPort)),
		Cert:   string(cert),
		Key:    string(key),
	})
	if err != nil {
		panic(err)
	}
	userdata := base64.StdEncoding.EncodeToString(data)

	// if not being forced up check if it exists
	if !args.Force {

		filterForce := &ec2.Filter{
			Name:   aws.String("name"),
			Values: []*string{aws.String(args.Name)},
		}
		var imagesForce *ec2.DescribeImagesOutput
		imagesForce, err = client.DescribeImages(&ec2.DescribeImagesInput{
			Filters: []*ec2.Filter{filterForce},
		})
		if err != nil {
			return err
		}
		if len(imagesForce.Images) > 0 {
			return errors.New("ami exists: try using the --force flag")
		}
	}
	fmt.Println("Looking up security group ID...")

	secgrps, err := client.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupNames: []*string{aws.String(securityGroupName)},
	})
	if err != nil {
		return err
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
		return fmt.Errorf("the %s security group must allow TCP ingress on port %d", securityGroupName, securityGroupPort)
	}

	securityGroupID := *secgrps.SecurityGroups[0].GroupId
	fmt.Printf("Security group: %s\n", securityGroupID)
	filter := &ec2.Filter{
		Name:   aws.String("name"),
		Values: []*string{aws.String("vorteil-compiler")},
	}
	images, err := client.DescribeImages(&ec2.DescribeImagesInput{
		Owners:  OwnerStrings,
		Filters: []*ec2.Filter{filter},
	})
	if err != nil {
		return err
	}

	gigs := vcfg.Bytes(args.Image.Size())
	gigs.Align(vcfg.GiB)

	fmt.Printf("Disk size: %d GiB\n", gigs.Units(vcfg.GiB))
	AMI = *images.Images[0].ImageId
	reservation, err := client.RunInstancesWithContext(args.Context, &ec2.RunInstancesInput{
		MaxCount:         aws.Int64(1),
		MinCount:         aws.Int64(1),
		ImageId:          aws.String(AMI),
		InstanceType:     aws.String(MachineType),
		UserData:         &userdata,
		SecurityGroupIds: []*string{&securityGroupID},
		BlockDeviceMappings: []*ec2.BlockDeviceMapping{
			&ec2.BlockDeviceMapping{
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
		return err
	}
	instanceID := *reservation.Instances[0].InstanceId

	fmt.Printf("Created empty instance: %s.\n", instanceID)

	var successful bool
	var amiID string

	defer func() {
		if successful {
			fmt.Printf("Provisioned AMI: %s\n", amiID)
		}
	}()

	defer func() {
		var err error
		fmt.Println("Attempting to terminate instance...\n")
		_, err = client.TerminateInstances(&ec2.TerminateInstancesInput{
			InstanceIds: []*string{
				&instanceID,
			},
		})
		if err != nil {
			fmt.Printf("An error occurred trying to clean up instance %s: %v.\n", instanceID, err)
		}
		fmt.Println("Instance terminated.")
	}()

	var ip string
	for {

		time.Sleep(pollrate)
		description, err := client.DescribeInstancesWithContext(args.Context, &ec2.DescribeInstancesInput{
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
			fmt.Println("Instance status: pending.")
			continue
		case 16:
			fmt.Println("Instance status: running.")
			if description == nil || len(description.Reservations) == 0 ||
				len(description.Reservations[0].Instances) == 0 ||
				len(description.Reservations[0].Instances[0].NetworkInterfaces) == 0 ||
				description.Reservations[0].Instances[0].NetworkInterfaces[0].Association == nil ||
				description.Reservations[0].Instances[0].NetworkInterfaces[0].Association.PublicIp == nil {
				continue
			}

			ip = *description.Reservations[0].Instances[0].NetworkInterfaces[0].Association.PublicIp
			if ip == "" {
				continue
			}
		case 32:
			fmt.Println("Instance status: shutting-down.")
			return errors.New("instance is shutting down for an unknown reason")
		case 48:
			fmt.Println("Instance status: terminated.")
			return errors.New("instance has been terminated for an unknown reason")
		case 64:
			fmt.Println("Instance status: stopping.")
			return errors.New("instance is stopping for an unknown reason")
		case 80:
			fmt.Println("Instance status: stopped.")
			return errors.New("instance stopped for an unknown reason")
		default:
			fmt.Println("Instance status: unknown.")
			continue
		}

		break
	}

	fmt.Printf("Instance public IP address: %s\n", ip)

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
	httpclient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("https://%s:%d/", ip, securityGroupPort), nil)
	if err != nil {
		return err
	}
	req = req.WithContext(args.Context)
	max := 6
	tries := 0
	gap := time.Second
	for {
		tries++
		fmt.Printf("Polling instance for connectivity (%d/%d)...\n", tries, max)
		ctx, cancel := context.WithTimeout(args.Context, time.Second*10)
		req = req.WithContext(ctx)
		resp, err := httpclient.Do(req)
		cancel()
		if err != nil {
			fmt.Printf("Trying %v out of %v\n", tries, max)
			fmt.Printf("Error on POST: %v\n", err)
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
		case <-args.Context.Done():
			// ?
		case <-time.After(gap):
		}
		gap *= 2
	}

	fmt.Println("Instance is live and ready for payload.")

	pr, pw := io.Pipe()
	defer pr.Close()
	gz := gzip.NewWriter(pw)
	hasher := md5.New()
	mw := io.MultiWriter(gz, hasher)
	// counter := datacounter.NewWriterCounter(mw)
	go func() {
		// _, _ = io.Copy(counter, args.Image)
		_, _ = io.Copy(mw, args.Image)
		gz.Close()
		pw.Close()
	}()

	req, err = http.NewRequest(http.MethodPost, fmt.Sprintf("https://%s:%d/", ip, securityGroupPort), pr)
	if err != nil {
		return err
	}

	// TODO: use server API to track disk write progress
	req.Header.Set("Disk-Size", fmt.Sprintf("%d", gigs.Units(1)))
	req = req.WithContext(args.Context)

	resp, err := httpclient.Do(req)
	if err != nil {
		return fmt.Errorf("error posting RAW image to instance: %v", err)
	}
	defer resp.Body.Close()

	fmt.Println("Payload dispatched.")
	checksum := hex.EncodeToString(hasher.Sum(nil))
	fmt.Printf("Our checksum: %s\n", checksum)

	data, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("server error [%d]: %s", resp.StatusCode, data)
	}

	fmt.Printf("Server checksum: %s\n", data)

	for {
		time.Sleep(pollrate)
		description, err := client.DescribeInstancesWithContext(args.Context, &ec2.DescribeInstancesInput{
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
			fmt.Println("Instance status: pending.")
			return errors.New("instance is restarting for an unknown reason")
		case 16:
			fmt.Println("Instance status: running.")
			continue
		case 32:
			fmt.Println("Instance status: shutting-down.")
			continue
		case 48:
			fmt.Println("Instance status: terminated.")
			return errors.New("instance has been terminated for an unknown reason")
		case 64:
			fmt.Println("Instance status: stopping.")
			continue
		case 80:
			fmt.Println("Instance status: stopped.")
		default:
			fmt.Println("Instance status: unknown.")
			continue
		}

		break
	}

	fmt.Println("Instance has stopped.")

	// make AMI
	// if args.Force, check if ami exists with same name
	if args.Force {
		filterForce := &ec2.Filter{
			Name:   aws.String("name"),
			Values: []*string{aws.String(args.Name)},
		}
		imagesForce, err := client.DescribeImages(&ec2.DescribeImagesInput{
			Filters: []*ec2.Filter{filterForce},
		})
		if err != nil {
			return err
		}
		if len(imagesForce.Images) > 0 {
			// deregister current live version as were force pushing
			fmt.Printf("deregistering old ami: %v\n", imagesForce.Images[0].ImageId)
			_, err := client.DeregisterImageWithContext(args.Context, &ec2.DeregisterImageInput{
				ImageId: imagesForce.Images[0].ImageId,
			})
			if err != nil {
				return err
			}
		}
	}

	img, err := client.CreateImageWithContext(args.Context, &ec2.CreateImageInput{
		Description: aws.String(args.Description),
		InstanceId:  aws.String(instanceID),
		Name:        aws.String(args.Name),
		// TODO: Block Device Mappings?
	})
	if err != nil {
		return err
	}

	fmt.Printf("AMI ID: %s\n", *img.ImageId)

	if args.ReadyWhenUsable {
		for {
			time.Sleep(pollrate)
			description, err := client.DescribeImagesWithContext(args.Context, &ec2.DescribeImagesInput{
				ImageIds: []*string{
					img.ImageId,
				},
			})
			if err != nil {
				return err
			}

			if description == nil || len(description.Images) == 0 {
				continue
			}
			fmt.Printf("Image status: %s.\n", *description.Images[0].State)
			if *description.Images[0].State == "available" {
				break
			}
		}
	}

	fmt.Printf("Provisioned AMI: %s\n", *img.ImageId)
	successful = true
	amiID = *img.ImageId

	err = nil
	return err
}

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
