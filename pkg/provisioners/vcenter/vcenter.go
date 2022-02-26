package vcenter

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/ovf"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/ova"
	"github.com/vorteil/vorteil/pkg/provisioners"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
)

// ProvisionerType : Constant string value used to represent the provisioner type vcenter
const ProvisionerType = "vcenter"

// Provisioner satisfies the provisioners.Provisioner interface
type Provisioner struct {
	cfg *Config
	log elog.View

	// vcenter
	client       *govmomi.Client
	datacenter   *object.Datacenter
	datastore    *object.Datastore
	finder       *find.Finder
	resourcepool *object.ResourcePool
	folder       *object.Folder
	filemanager  *object.DatastoreFileManager
	args         provisioners.ProvisionArgs
}

// Config contains configuration fields required by the Provisioner
type Config struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	Address    string `json:"address"`
	Datacenter string `json:"datacenter"`
	Datastore  string `json:"datastore"`
	Cluster    string `json:"cluster"`
	Notes      string `json:"notes"`
}

// NewProvisioner - Create a VCenter Provisioner object
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

// Validate Provisioner configuration
func (p *Provisioner) Validate() error {
	loginURL, err := url.Parse(p.cfg.Address + "/sdk")
	if err != nil {
		return err
	}
	loginURL.User = url.UserPassword(p.cfg.Username, p.cfg.Password)

	vclient, err := govmomi.NewClient(context.Background(), loginURL, true)
	if err != nil {
		return err
	}

	ctx := context.Background()

	finder := find.NewFinder(vclient.Client, true)
	datacenter, err := finder.DatacenterOrDefault(ctx, p.cfg.Datacenter)
	if err != nil {
		return err
	}
	finder = finder.SetDatacenter(datacenter)

	_, err = finder.DatastoreOrDefault(ctx, p.cfg.Datastore)
	if err != nil {
		return err
	}

	path := "/" + p.cfg.Datacenter + "/host/" + p.cfg.Cluster + "/Resources"
	_, err = finder.ResourcePoolOrDefault(ctx, path)
	if err != nil {
		return err
	}

	path = "/" + p.cfg.Datacenter + "/vm"
	_, err = finder.FolderOrDefault(ctx, path)
	return err
}

// init / validate the provisioner
func (p *Provisioner) init() error {
	ctx := context.Background()

	// TODO test connection
	loginURL, err := url.Parse(p.cfg.Address + "/sdk")
	if err != nil {
		return err
	}
	loginURL.User = url.UserPassword(p.cfg.Username, p.cfg.Password)

	p.client, err = govmomi.NewClient(context.Background(), loginURL, true)
	if err != nil {
		return err
	}

	p.finder = find.NewFinder(p.client.Client, true)
	p.datacenter, err = p.finder.DatacenterOrDefault(ctx, p.cfg.Datacenter)
	if err != nil {
		return err
	}
	p.finder = p.finder.SetDatacenter(p.datacenter)

	p.datastore, err = p.finder.DatastoreOrDefault(ctx, p.cfg.Datastore)
	if err != nil {
		return err
	}

	path := "/" + p.cfg.Datacenter + "/host/" + p.cfg.Cluster + "/Resources"
	p.resourcepool, err = p.finder.ResourcePoolOrDefault(ctx, path)
	if err != nil {
		return err
	}

	path = "/" + p.cfg.Datacenter + "/vm"
	p.folder, err = p.finder.FolderOrDefault(ctx, path)
	return err
}

// Type returns 'vcenter'
func (p *Provisioner) Type() string {
	return ProvisionerType
}

// DiskFormat returns the provisioners required disk format
func (p *Provisioner) DiskFormat() vdisk.Format {
	return ova.ImageFormatOVA
}

// SizeAlign returns vcfg MiB size in bytes
func (p *Provisioner) SizeAlign() vcfg.Bytes {
	return vcfg.MiB
}

// Provision given a valid ProvisionArgs object will provision the passed vorteil project
//	to the configured vcenter provisioner. OVF and VMDK will be needed in different steps
//	so the OVA tar will be read from directly.
func (p *Provisioner) Provision(args *provisioners.ProvisionArgs) error {
	var err error

	// report that the 'description' flag is ignored if using this provisioner
	if args.Description != "" {
		p.log.Warnf("WARN: The 'description' field is ignored by vcenter provision operations.")
	}

	// check for conflicts
	var vm *object.VirtualMachine
	vm, err = p.finder.VirtualMachine(args.Context, args.Name)
	if err == nil {
		// no error returned means a vm with that name exists already
		if args.Force {
			p.log.Printf("Force replacing disk '%s'.", args.Name)
			// delete the conflicting image
			var task *object.Task
			task, err = vm.PowerOff(args.Context)
			if err == nil {
				task.Wait(args.Context)
			}

			task, err = vm.Destroy(args.Context)
			if err != nil {
				return err
			}

			err = task.Wait(args.Context)
			if err != nil {
				return err
			}
		} else {
			// no force directive but conflict exists
			err = fmt.Errorf("'%s' already exists, use force flag to override", args.Name)
			return err
		}
	}

	tr := tar.NewReader(args.Image)

	// Read OVF from OVA tar
	hdr, err := tr.Next()
	if err == io.EOF {
		return fmt.Errorf("Could not unpack OVF from OVA IMAGE: %v", err)
	}

	if !strings.HasSuffix(hdr.FileInfo().Name(), ".ovf") {
		return fmt.Errorf("Could not unpack OVF from OVA IMAGE: 'First File '%s' is not a ovf", hdr.FileInfo().Name())
	}
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, tr)
	if err != nil {
		return err
	}

	importSpecParams := types.OvfCreateImportSpecParams{
		DiskProvisioning:   "THIN",
		EntityName:         args.Name,
		IpAllocationPolicy: "DHCP",
		IpProtocol:         "IPV4",
		OvfManagerCommonParams: types.OvfManagerCommonParams{
			DeploymentOption: "",
			Locale:           "US",
		},
		PropertyMapping: make([]types.KeyValue, 0),
		NetworkMapping:  make([]types.OvfNetworkMapping, 0),
	}

	p.log.Infof("Creating import spec...")
	ovfManager := ovf.NewManager(p.client.Client)
	importSpec, err := ovfManager.CreateImportSpec(args.Context, buf.String(),
		p.resourcepool, p.datastore, importSpecParams)
	if err != nil {
		return fmt.Errorf("could not Create Import Spec, %v", err.Error())
	}
	if importSpec.Error != nil {
		err = errors.New(importSpec.Error[0].LocalizedMessage)
		return fmt.Errorf("error on Create Import Spec, %v", err.Error())
	}
	if importSpec.Warning != nil {
		for _, w := range importSpec.Warning {
			warnMsg := w.LocalizedMessage
			// False positive warning ignored
			if !strings.HasSuffix(warnMsg, "for element 'Connection'.") {
				p.log.Warnf("WARN: %s", warnMsg)
			}
		}
	}

	p.log.Infof("Importing VApp...")
	lease, err := p.resourcepool.ImportVApp(args.Context,
		importSpec.ImportSpec, p.folder, nil)
	if err != nil {
		return err
	}

	p.log.Infof("Waiting on lease...")
	info, err := lease.Wait(context.Background(), importSpec.FileItem)
	if err != nil {
		if err.Error() == fmt.Sprintf("The name '%s' already exists.", args.Name) {
			return err
		}
	}

	// Read VMDK from OVA tar
	hdr, err = tr.Next()
	if err == io.EOF {
		return fmt.Errorf("Could not unpack VMDK from OVA IMAGE: %v", err)
	}

	if !strings.HasSuffix(hdr.FileInfo().Name(), ".vmdk") {
		return fmt.Errorf("Could not unpack VMDK from OVA IMAGE: 'Second File '%s' is not a .vmdk", hdr.FileInfo().Name())
	}

	updater := lease.StartUpdater(args.Context, info)
	defer updater.Done()
	upload := p.log.NewProgress("Uploading disk", "KiB", hdr.FileInfo().Size())
	err = lease.Upload(args.Context, info.Items[0], upload.ProxyReader(tr), soap.Upload{})
	if err != nil {
		return err
	}

	p.log.Printf("Getting virtual machine reference...")
	v, err := p.finder.VirtualMachine(args.Context, args.Name)
	if err != nil {
		return err
	}

	err = lease.Complete(args.Context)
	if err != nil {
		return err
	}

	p.log.Printf("Marking as virtual machine template...")
	v.MarkAsTemplate(args.Context)

	p.log.Printf("Provisioned: %s", args.Name)
	return nil
}

// Marshal returns json provisioner as bytes
func (p *Provisioner) Marshal() ([]byte, error) {
	m := make(map[string]interface{})
	m[provisioners.MapKey] = ProvisionerType
	m["username"] = p.cfg.Username
	m["password"] = p.cfg.Password
	m["address"] = p.cfg.Address
	m["datacenter"] = p.cfg.Datacenter
	m["datastore"] = p.cfg.Datastore
	m["cluster"] = p.cfg.Cluster
	m["notes"] = p.cfg.Notes

	out, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}

	return out, nil
}
