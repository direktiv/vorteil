// +build linux

package firecracker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"

	dhcp "github.com/krolaw/dhcp4"
	conn "github.com/krolaw/dhcp4/conn"
	dhcpHandler "github.com/vorteil/vorteil/pkg/virtualizers/dhcp"

	"github.com/milosgajdos/tenus"
	"github.com/songgao/water"
)

// CreateDevices is a json body that we read in when we need to create the tap devices for firecracker
type CreateDevices struct {
	ID     string `json:"id"`
	Routes int    `json:"count"`
}

// Devices is a return type which returns an array of tap devices created to the virtualizer so when we clean up we can use it.
type Devices struct {
	Devices []string `json:"devices"`
}

// Write implements the io.Writer interface.
// Always completes and never returns an error.
func (wc *writeCounter) Write(p []byte) (n int, e error) {
	n = len(p)
	wc.downloaded += int64(n)
	wc.onProgress(wc.downloaded, wc.total)
	return
}
func newWriter(size int64, onProgress func(downloaded, total int64)) io.Writer {
	return &writeCounter{total: size, onProgress: onProgress}
}

// ByteCountDecimal converts bytes to readable format
func ByteCountDecimal(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}

// FetchBridgeDev retrieves the bridge device
func FetchBridgeDev() (tenus.Bridger, error) {
	// Check if bridge device exists
	bridger, err := tenus.BridgeFromName("vorteil-bridge")
	if err != nil {
		return nil, errors.New("try running 'vorteil firecracker-setup' before using firecracker")
	}
	return bridger, err
}

// CreateBridgeDevice creates a bridge device if it already exists returns with the bridge device
func CreateBridgeDevice() (tenus.Bridger, error) {
	// Create bridge device
	bridger, err := tenus.NewBridgeWithName("vorteil-bridge")
	if err != nil {
		if !strings.Contains(err.Error(), "Interface name vorteil-bridge already assigned on the host") {
			return nil, err
		}
		// get bridge device
		bridger, err = FetchBridgeDev()
		if err != nil {
			return nil, err
		}
	}
	// Switch bridge up
	if err = bridger.SetLinkUp(); err != nil {
		return nil, err
	}
	return bridger, nil
}

//SetupBridgeAndDHCPServer initializes the bridge device and creates a dhcp & http handler to create devices.
func SetupBridgeAndDHCPServer() error {

	// Create Bridge device and assign dhcp ips to it
	bridger, err := CreateBridgeDevice()
	if err != nil {
		return err
	}
	// Fetch address
	ipv4Addr, ipv4Net, err := net.ParseCIDR("174.72.0.1/24")
	if err != nil {
		if !strings.Contains(err.Error(), "file exists") {
			return err
		}
	}
	// Assign bridge to device so host knows where to send requests.
	if err = bridger.SetLinkIp(ipv4Addr, ipv4Net); err != nil {
		if !strings.Contains(err.Error(), "file exists") {
			return err
		}
	}
	// create dhcp server on an interface
	server := dhcpHandler.NewHandler()
	pc, err := conn.NewUDP4BoundListener("vorteil-bridge", ":67")
	if err != nil {
		return err
	}

	// create server handler to create tap devices under sudo
	http.HandleFunc("/", OrganiseTapDevices)
	go func() {
		http.ListenAndServe(":7476", nil)
	}()
	// Start dhcp server to listen
	dhcp.Serve(pc, server)

	return nil
}

// OrganiseTapDevices is a handler which manages the tap devices creates them, deletes them while listening on localhost:7476
func OrganiseTapDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var cd CreateDevices
		var tapDevices []string

		// Decode json body
		err := json.NewDecoder(r.Body).Decode(&cd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		// get bridge device
		bridgeDev, err := FetchBridgeDev()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		// set network adapters
		if cd.Routes > 0 {
			for i := 0; i < cd.Routes; i++ {
				ifceName := fmt.Sprintf("%s-%s", cd.ID, strconv.Itoa(i))

				// create tap device
				config := water.Config{
					DeviceType: water.TAP,
				}
				config.Name = ifceName
				config.Persist = true
				ifce, err := water.New(config)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}
				// close interface so firecracker can read it
				err = ifce.Close()
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}

				// get tap device
				linkDev, err := tenus.NewLinkFrom(ifceName)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}
				//set tap device up
				err = linkDev.SetLinkUp()
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}
				// add network interface to bridge
				err = bridgeDev.AddSlaveIfc(linkDev.NetInterface())
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
				}
				tapDevices = append(tapDevices, ifceName)
			}
			// write interfaces back
			returnDevices := &Devices{
				Devices: tapDevices,
			}
			body, err := json.Marshal(returnDevices)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)

			}
			io.Copy(w, bytes.NewBuffer(body))
		}
	case http.MethodDelete:
		var dd Devices
		err := json.NewDecoder(r.Body).Decode(&dd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		for i := 0; i < len(dd.Devices); i++ {
			err := tenus.DeleteLink(dd.Devices[i])
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not available", http.StatusBadRequest)
	}
}

// Write method to handle logging from firecracker to use our logger interface
// Cant use logger interface as it duplicates
func (v *Virtualizer) Write(d []byte) (n int, err error) {
	n = len(d)
	v.logger.Infof(string(d))
	return
}

// writeCounter counts the number of bytes written to it.
type writeCounter struct {
	total      int64 // total size
	downloaded int64 // downloaded # of bytes transferred
	onProgress func(downloaded int64, total int64)
}

// Detach ... detaches a virtual machine from the daemon
// func (v *Virtualizer) Detach(source string) error {
// 	if v.state != virtualizers.Ready {
// 		return errors.New("virtual machine must be in ready state to detach")
// 	}
// 	name := filepath.Base(v.folder)

// 	err := os.MkdirAll(filepath.Join(source), 0777)
// 	if err != nil {
// 		return err
// 	}
// 	err = os.Rename(v.folder, filepath.Join(source, name))
// 	if err != nil {
// 		return err
// 	}

// 	v.config.VM.RAM.Align(vcfg.MiB * 2)

// 	// close and cleanup tap devices
// 	// stopVMM
// 	err = v.machine.StopVMM()
// 	if err != nil {
// 		return err
// 	}

// 	// sleep for shutdown signal
// 	time.Sleep(time.Second * 4)

// 	v.state = virtualizers.Deleted

// 	cleanup, err := os.Create(filepath.Join(source, name, "cleanup.sh"))
// 	if err != nil {
// 		return err
// 	}
// 	defer cleanup.Close()
// 	var tapArgs []string
// 	var tapCreation []string
// 	type NetworkInterface struct {
// 		IfaceID     string `json:"iface_id"`
// 		HostDevName string `json:"host_dev_name"`
// 	}
// 	var networkCards []NetworkInterface
// 	// write script for Tap setup
// 	if len(v.routes) > 0 {
// 		for i := 0; i < len(v.routes); i++ {
// 			ifceName := fmt.Sprintf("%s-%s", v.id, strconv.Itoa(i))
// 			tapCreation = append(tapCreation, ifceName)
// 		}
// 		for _, tap := range tapCreation {
// 			tapArgs = append(tapArgs, fmt.Sprintf("sudo ip tuntap add dev %s mode tap", tap))
// 			tapArgs = append(tapArgs, fmt.Sprintf("sudo brctl addif vorteil-bridge %s", tap))
// 			tapArgs = append(tapArgs, fmt.Sprintf("sudo ip link set dev %s up", tap))
// 			cleanup.Write([]byte(fmt.Sprintf("sudo ip link delete %s\n", tap)))

// 			networkCards = append(networkCards, NetworkInterface{
// 				IfaceID:     "vorteil-bridge",
// 				HostDevName: tap,
// 			})
// 		}
// 	}
// 	f, err := os.Create(filepath.Join(source, name, "interfaces.sh"))
// 	if err != nil {
// 		return err
// 	}
// 	f.Write([]byte(strings.Join(tapArgs, "\n")))
// 	defer f.Close()

// 	type Drive struct {
// 		DriveID      string `json:"drive_id"`
// 		PathOnHost   string `json:"path_on_host"`
// 		IsRootDevice bool   `json:"is_root_device"`
// 		IsReadOnly   bool   `json:"is_read_only"`
// 	}
// 	type BootSource struct {
// 		KernelImagePath string `json:"kernel_image_path"`
// 		BootArgs        string `json:"boot_args"`
// 	}
// 	type MachineConfig struct {
// 		VcpuCount  int  `json:"vcpu_count"`
// 		MemSizeMib int  `json:"mem_size_mib"`
// 		HtEnabled  bool `json:"ht_enabled"`
// 	}
// 	type fcConfig struct {
// 		BootSource        BootSource         `json:"boot-source"`
// 		Drives            []Drive            `json:"drives"`
// 		MachineConfig     MachineConfig      `json:"machine-config"`
// 		NetworkInterfaces []NetworkInterface `json:"network-interfaces"`
// 	}

// 	drive := Drive{
// 		DriveID:      "rootfs",
// 		PathOnHost:   filepath.Join(source, name, fmt.Sprintf("%s.raw", v.name)),
// 		IsRootDevice: true,
// 		IsReadOnly:   false,
// 	}
// 	var drives []Drive
// 	drives = append(drives, drive)

// 	var config fcConfig
// 	config.Drives = drives
// 	config.BootSource = BootSource{
// 		KernelImagePath: v.kip,
// 		BootArgs:        "init=/vorteil/vinitd reboot=k panic=1 pci=off i8042.noaux i8042.nomux i8042.nopnp i8042.dumbkbd  vt.color=0x00",
// 	}
// 	config.MachineConfig = MachineConfig{
// 		VcpuCount:  int(v.config.VM.CPUs),
// 		HtEnabled:  false,
// 		MemSizeMib: v.config.VM.RAM.Units(vcfg.MiB),
// 	}
// 	config.NetworkInterfaces = networkCards

// 	data, err := json.Marshal(config)
// 	if err != nil {
// 		return err
// 	}

// 	jf, err := os.Create(filepath.Join(source, name, "config.json"))
// 	if err != nil {
// 		return err
// 	}
// 	defer jf.Close()
// 	jf.Write(data)

// 	start, err := os.Create(filepath.Join(source, name, "start.sh"))
// 	if err != nil {
// 		return err
// 	}
// 	defer start.Close()
// 	start.Write([]byte("sudo ./interfaces.sh\nfirecracker --api-sock ./firecracker.socket --config-file ./config.json"))

// 	// Chmod scripts
// 	err = os.Chmod(start.Name(), 0777)
// 	if err != nil {
// 		return err
// 	}
// 	err = os.Chmod(f.Name(), 0777)
// 	if err != nil {
// 		return err
// 	}
// 	err = os.Chmod(cleanup.Name(), 0777)
// 	if err != nil {
// 		return err
// 	}

// 	// remove virtualizer from active vms
// 	virtualizers.ActiveVMs.Delete(v.name)
// 	return nil
// }

// CreateTapDevices creates devices required for the virtual machine to run
func CreateTapDevices(id string, routes int) (*Devices, error) {
	cd := CreateDevices{
		ID:     id,
		Routes: routes,
	}

	cdm, err := json.Marshal(cd)
	if err != nil {
		return nil, err
	}
	resp, err := http.Post("http://localhost:7476/", "application/json", bytes.NewBuffer(cdm))
	if err != nil {
		return nil, errors.New("Run ./sudo firecracker-setup for the listener")
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ifs Devices
	err = json.Unmarshal(body, &ifs)
	if err != nil {
		return nil, err
	}
	return &ifs, nil
}

// DeleteTapDevices delete the tap devices using the listener
func DeleteTapDevices(devices []string) error {
	client := &http.Client{}
	cdm, err := json.Marshal(devices)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("DELETE", "http://localhost:7476/", bytes.NewBuffer(cdm))
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}
