// +build linux

package virtualizers

import (
	"net"
	"strings"

	dhcp "github.com/krolaw/dhcp4"
	conn "github.com/krolaw/dhcp4/conn"
	"github.com/milosgajdos/tenus"
	dhcpHandler "github.com/vorteil/vorteil/pkg/virtualizers/dhcp"
)

func SetupBridgeAndDHCPServer() error {

	// Create bridge device
	bridger, err := tenus.NewBridgeWithName("vorteil-bridge")
	if err != nil {
		if !strings.Contains(err.Error(), "Interface name vorteil-bridge already assigned on the host") {
			return err
		}
		// get bridge device
		bridger, err = tenus.BridgeFromName("vorteil-bridge")
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
	// create dhcp server on an interface
	server := dhcpHandler.NewHandler()
	pc, err := conn.NewUDP4BoundListener("vorteil-bridge", ":67")
	if err != nil {
		return err
	}

	// Start dhcp server to listen
	go func() {
		dhcp.Serve(pc, server)
	}()

	return nil
}
