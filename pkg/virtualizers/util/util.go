package util

import (
	"strings"
	"time"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
)

// LookForIP screen scrapes the IP from a virtual machine output mainly used for bridge/hosted machines
func LookForIP(l *logger.Logger) []string {

	sub := l.Subscribe()
	inbox := sub.Inbox()
	var msg string
	timer := false
	msgWrote := false
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case logdata, _ := <-inbox:
			msg += string(logdata)
			if strings.TrimSpace(msg) != "" && strings.Contains(msg, "ip") {
				msgWrote = true
			}
		case <-ticker.C:
			if msgWrote {
				// sleep slightly so we get all the IPS
				time.Sleep(time.Second * 1)
				timer = true
			}
		// after 30 seconds break out of for loop for memory resolving
		case <-time.After(time.Second * 30):
			timer = true
		}
		if timer {
			break
		}
	}
	var ips []string
	lines := strings.Split(msg, "\r\n")
	for _, line := range lines {
		if virtualizers.IPRegex.MatchString(line) {
			if strings.Contains(line, "ip") {
				split := strings.Split(line, ":")
				if len(split) > 1 {
					ips = append(ips, strings.TrimSpace(split[1]))
				}
			}
		}
	}
	return ips
}

// Routes converts networks from the config to readable virtualizers.NetworkInterface fields.
func Routes(cfg *vcfg.VCFG) []virtualizers.NetworkInterface {

	routes := virtualizers.Routes{}
	var nics = cfg.Networks
	for i, nic := range nics {
		if nic.IP == "" {
			continue
		}
		protocols := []string{
			"udp",
			"tcp",
			"http",
			"https",
		}
		portLists := [][]string{
			nic.UDP,
			nic.TCP,
			nic.HTTP,
			nic.HTTPS,
		}
		for j := 0; j < len(protocols); j++ {
			protocol := protocols[j]
			ports := portLists[j]
			if routes.NIC[i].Protocol == nil {
				routes.NIC[i].Protocol = make(map[virtualizers.NetworkProtocol]*virtualizers.NetworkProtocolPorts)
			}
			if protocol == "" {
				protocol = "http"
			}
			p := virtualizers.NetworkProtocol(protocol)
			existingPorts, ok := routes.NIC[i].Protocol[p]
			if !ok {
				existingPorts = &virtualizers.NetworkProtocolPorts{
					Port: make(map[string]*virtualizers.NetworkRoute),
				}
			}
			for _, port := range ports {
				existingPorts.Port[port] = new(virtualizers.NetworkRoute)
			}
			routes.NIC[i].Protocol[p] = existingPorts
		}
	}
	apiNics := make([]virtualizers.NetworkInterface, 0)
	for i, net := range cfg.Networks {
		newNetwork := virtualizers.NetworkInterface{
			Name:    "",
			IP:      net.IP,
			Mask:    net.Mask,
			Gateway: net.Gateway,
		}
		for _, port := range net.UDP {
			var addr string
			if len(routes.NIC) > i {
				nic := routes.NIC[i]
				if proto, ok := nic.Protocol["udp"]; ok {
					if pmap, ok := proto.Port[port]; ok {
						addr = pmap.Address
					}
				}
			}
			newNetwork.UDP = append(newNetwork.UDP, virtualizers.RouteMap{
				Port:    port,
				Address: addr,
			})
		}
		for _, port := range net.TCP {
			var addr string
			if len(routes.NIC) > i {
				nic := routes.NIC[i]
				if proto, ok := nic.Protocol["tcp"]; ok {
					if pmap, ok := proto.Port[port]; ok {
						addr = pmap.Address
					}
				}
			}
			newNetwork.TCP = append(newNetwork.TCP, virtualizers.RouteMap{
				Port:    port,
				Address: addr,
			})
		}
		for _, port := range net.HTTP {
			var addr string
			if len(routes.NIC) > i {
				nic := routes.NIC[i]
				if proto, ok := nic.Protocol["http"]; ok {
					if pmap, ok := proto.Port[port]; ok {
						addr = pmap.Address
					}
				}
			}
			newNetwork.HTTP = append(newNetwork.HTTP, virtualizers.RouteMap{
				Port:    port,
				Address: addr,
			})
		}
		for _, port := range net.HTTPS {
			var addr string
			if len(routes.NIC) > i {
				nic := routes.NIC[i]
				if proto, ok := nic.Protocol["https"]; ok {
					if pmap, ok := proto.Port[port]; ok {
						addr = pmap.Address
					}
				}
			}
			newNetwork.HTTPS = append(newNetwork.HTTPS, virtualizers.RouteMap{
				Port:    port,
				Address: addr,
			})
		}
		apiNics = append(apiNics, newNetwork)
	}
	return apiNics
}
