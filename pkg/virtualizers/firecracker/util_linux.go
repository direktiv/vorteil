// +build linux

package firecracker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"

	dhcp "github.com/krolaw/dhcp4"
	conn "github.com/krolaw/dhcp4/conn"
	"github.com/milosgajdos/tenus"
	"github.com/songgao/water"
	"github.com/vorteil/vorteil/pkg/elog"
	dhcpHandler "github.com/vorteil/vorteil/pkg/virtualizers/dhcp"
)

// FetchBridgeDev attempts to retrieve the bridge device
func FetchBridgeDev() error {
	// Check if bridge device exists
	_, err := tenus.BridgeFromName(vorteilBridge)
	if err != nil {
		return errors.New("try running 'vorteil firecracker-setup' before using firecracker")
	}
	return err
}

// SetupBridgeAndDHCPServer creates the bridge which provides DHCP addresses todo
// firecracker instances.
func SetupBridgeAndDHCPServer(log elog.View) error {

	log.Printf("creating bridge %s", vorteilBridge)
	// Create bridge device
	bridger, err := tenus.NewBridgeWithName(vorteilBridge)
	if err != nil {
		if !strings.Contains(err.Error(), "Interface name vorteil-bridge already assigned on the host") {
			return err
		}
		// get bridge device
		bridger, err = tenus.BridgeFromName(vorteilBridge)
		if err != nil {
			return err
		}
	}
	// Switch bridge up
	if err = bridger.SetLinkUp(); err != nil {
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

	log.Printf("starting dhcp server")

	// create dhcp server on an interface
	server := dhcpHandler.NewHandler()
	pc, err := conn.NewUDP4BoundListener(vorteilBridge, ":67")
	if err != nil {
		return err
	}

	// create server handler to create tap devices under sudo
	http.HandleFunc("/", OrganiseTapDevices)
	go func() {
		http.ListenAndServe(":7476", nil)
	}()
	log.Printf("Listening on '7476' for creating and deleting TAP devices")
	log.Printf("Listening on 'vorteil-bridge' for DHCP requests")
	// Start dhcp server to listen
	dhcp.Serve(pc, server)

	return nil
}

// CreateDevices is a struct used to tell the process to create TAP devices via a rest request
type CreateDevices struct {
	Id     string `json:"id"`
	Routes int    `json:"count"`
}

// Devices is a struct used to tell the process to deleted TAP devices via a delete request
type Devices struct {
	Devices []string `json:"devices"`
}

func createTaps(w http.ResponseWriter, cd CreateDevices, t tenus.Bridger) []string {
	var tapDevices []string

	for i := 0; i < cd.Routes; i++ {
		ifceName := fmt.Sprintf("%s-%s", cd.Id, strconv.Itoa(i))

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
		err = t.AddSlaveIfc(linkDev.NetInterface())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		tapDevices = append(tapDevices, ifceName)
	}

	return tapDevices
}

func createDevices(w http.ResponseWriter, r *http.Request) {
	var cd CreateDevices

	err := json.NewDecoder(r.Body).Decode(&cd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}

	// get bridge device
	bridgeDev, err := tenus.BridgeFromName(vorteilBridge)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}

	// set network adapters
	if cd.Routes > 0 {
		// write interfaces back
		returnDevices := &Devices{
			Devices: createTaps(w, cd, bridgeDev),
		}
		body, err := json.Marshal(returnDevices)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

		}
		io.Copy(w, bytes.NewBuffer(body))
	}
}

// OrganiseTapDevices handles http requests to create and delete tap interfaces for firecracker
func OrganiseTapDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		createDevices(w, r)
	case http.MethodDelete:
		deleteDevices(w, r)
	default:
		http.Error(w, "method not available", http.StatusBadRequest)
	}
}

func deleteDevices(w http.ResponseWriter, r *http.Request) {
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
}
