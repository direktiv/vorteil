package nutanix

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/provisioners"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
)

// ProvisionerType : Constant string value used to represent the provisioner type nutanix
const ProvisionerType = "nutanix"

// Provisioner satisfies the provisioners.Provisioner interface
type Provisioner struct {
	cfg    *Config
	log    elog.View
	client http.Client
}

// Config contains configuration fields required by the Provisioner
type Config struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
}

// NewProvisioner creates a provisioner object that it returns
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

// Validate arguments for nutanix
func (p *Provisioner) Validate() error {
	if p.cfg.Username == "" || p.cfg.Password == "" || p.cfg.Host == "" {
		return fmt.Errorf("username,password and host fields should not be empty")
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	authHeader := fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", p.cfg.Username, p.cfg.Password))))

	p.client = http.Client{Transport: tr}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("https://%s/api/nutanix/v3/users/me", p.cfg.Host), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authHeader)

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unable to check status of nutanix user: status %v", resp.StatusCode)
	}

	return nil
}

// init
func (p *Provisioner) init() error {
	return nil
}

// Type returns 'nutanix'
func (p *Provisioner) Type() string {
	return ProvisionerType
}

// DiskFormat returns the provisioners required disk format
func (p *Provisioner) DiskFormat() vdisk.Format {
	return vdisk.VMDKSparseFormat
}

// SizeAlign returns VCFG GiB size in bytes
func (p *Provisioner) SizeAlign() vcfg.Bytes {
	return vcfg.MiB
}

// Provision given a valid ProvisionArgs object will provision the passed vorteil project
//	to the configured provisioner
func (p *Provisioner) Provision(args *provisioners.ProvisionArgs) error {

	authHeader := fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", p.cfg.Username, p.cfg.Password))))
	if args.Description != "" {
		p.log.Warnf(`The 'description' field is ignored by Nutanix provision operation`)
	}

	p.log.Infof("Checking for image name conflicts...")

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("https://%s/api/nutanix/v3/images/list", p.cfg.Host), strings.NewReader(fmt.Sprintf("{\n\t\"kind\":\"image\",\n\t\"length\":1000\n}")))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list images response was not ok: %s", resp.Status)
	}

	imagesList := new(listImagesResponse)
	err = json.NewDecoder(resp.Body).Decode(&imagesList)
	if err != nil {
		return err
	}
	resp.Body.Close()

	var isFound bool
	for _, i := range imagesList.Entities {
		if i.Spec.Name == args.Name {
			isFound = true
			break
		}
	}

	if isFound {
		if args.Force {
			p.log.Warnf("An image with this name already exists; this will not affect the current operation.")
		}
	}

	p.log.Infof("Sending 'create image' request")

	reqBody := fmt.Sprintf("{\n\t\"spec\": {\n\t\t\"name\": \"%s\",\n\t\t\"resources\": {\n\t\t\t\"image_type\": \"DISK_IMAGE\"\n\t\t}\n\t},\n\t\"metadata\": {\n\t\t\"kind\": \"image\"\n\t}\n}", args.Name)
	req, err = http.NewRequest(http.MethodPost, fmt.Sprintf("https://%s/api/nutanix/v3/images", p.cfg.Host), strings.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/json")

	resp, err = p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("create image response was not ok: %s", resp.Status)
	}

	response := new(imageCreateResponse)
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		return err
	}

	cookie := resp.Cookies()[0]
	resp.Body.Close()

	uuid := response.Metadata.UUID
	if uuid == "" {
		return fmt.Errorf("unable to read image uuid from api response")
	}

	// Ends up 404ing without a delay
	time.Sleep(time.Second * 1)

	p.log.Infof("Uploading disk to image (uuid: '%s')", uuid)
	pe := p.log.NewProgress("Uploading", "KiB", int64(args.Image.Size()))

	req, err = http.NewRequest(http.MethodPut, fmt.Sprintf("https://%s/api/nutanix/v3/images/%s/file", p.cfg.Host, uuid), pe.ProxyReader(args.Image))
	if err != nil {
		return err
	}

	req.AddCookie(cookie)
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(args.Image.Size())
	req.Header.Set("Accept", "*/*")

	resp, err = p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		p.log.Infof("expected status 200 from nutanix server, got '%v'", resp.StatusCode)
		b, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("%s", b)
	}
	p.log.Infof("Done!")
	return nil
}

// Marshal returns json provisioner as bytes
func (p *Provisioner) Marshal() ([]byte, error) {
	m := make(map[string]interface{})
	m[provisioners.MapKey] = ProvisionerType
	m["username"] = p.cfg.Username
	m["password"] = p.cfg.Password
	m["host"] = p.cfg.Host

	out, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	return out, nil
}

type imageCreateResponse struct {
	Status struct {
		State            string `json:"state"`
		ExecutionContext struct {
			TaskUUID string `json:"task_uuid"`
		} `json:"execution_context"`
	} `json:"status"`
	Spec struct {
		Name      string `json:"name"`
		Resources struct {
			ImageType string `json:"image_type"`
		} `json:"resources"`
	} `json:"spec"`
	APIVersion string `json:"api_version"`
	Metadata   struct {
		OwnerReference struct {
			Kind string `json:"kind"`
			UUID string `json:"uuid"`
			Name string `json:"name"`
		} `json:"owner_reference"`
		UseCategoriesMapping bool   `json:"use_categories_mapping"`
		Kind                 string `json:"kind"`
		SpecVersion          int    `json:"spec_version"`
		UUID                 string `json:"uuid"`
	} `json:"metadata"`
}

type listImagesResponse struct {
	APIVersion string `json:"api_version"`
	Metadata   struct {
		TotalMatches int    `json:"total_matches"`
		Kind         string `json:"kind"`
		Length       int    `json:"length"`
		Offset       int    `json:"offset"`
	} `json:"metadata"`
	Entities []struct {
		Status struct {
			State            string `json:"state"`
			ExecutionContext struct {
				TaskUuids []string `json:"task_uuids"`
			} `json:"execution_context"`
			Name      string `json:"name"`
			Resources struct {
				RetrievalURIList []string `json:"retrieval_uri_list"`
				ImageType        string   `json:"image_type"`
				SourceURI        string   `json:"source_uri"`
				Architecture     string   `json:"architecture"`
				SizeBytes        int      `json:"size_bytes"`
			} `json:"resources"`
		} `json:"status"`
		Spec struct {
			Name      string `json:"name"`
			Resources struct {
				ImageType    string `json:"image_type"`
				SourceURI    string `json:"source_uri"`
				Architecture string `json:"architecture"`
			} `json:"resources"`
		} `json:"spec"`
		Metadata struct {
			LastUpdateTime    time.Time `json:"last_update_time"`
			Kind              string    `json:"kind"`
			UUID              string    `json:"uuid"`
			SpecVersion       int       `json:"spec_version"`
			CreationTime      time.Time `json:"creation_time"`
			CategoriesMapping struct {
			} `json:"categories_mapping"`
			OwnerReference struct {
				Kind string `json:"kind"`
				UUID string `json:"uuid"`
				Name string `json:"name"`
			} `json:"owner_reference"`
			Categories struct {
			} `json:"categories"`
		} `json:"metadata"`
	} `json:"entities"`
}
