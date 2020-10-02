package util

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"
	"strings"
	"time"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
)

// ConvertToVM converts a virtualizer into a machine struct mainly used for api returning.
func ConvertToVM(name string, pname string, state string, routes []virtualizers.NetworkInterface, created time.Time, cfg *vcfg.VCFG, source interface{}) interface{} {
	info := cfg.Info
	vm := cfg.VM
	system := cfg.System
	programs := make([]virtualizers.ProgramSummaries, 0)

	for _, p := range cfg.Programs {
		programs = append(programs, virtualizers.ProgramSummaries{
			Binary: p.Binary,
			Args:   string(p.Args),
			Env:    p.Env,
		})
	}

	machine := &virtualizers.VirtualMachine{
		ID:       name,
		Author:   info.Author,
		CPUs:     int(vm.CPUs),
		RAM:      vm.RAM,
		Disk:     vm.DiskSize,
		Created:  created,
		Date:     info.Date.Time(),
		Networks: routes,
		Kernel:   vm.Kernel,
		Name:     info.Name,
		Summary:  info.Summary,
		// Source:   source.(virtualizers.Source),
		URL:      string(info.URL),
		Version:  info.Version,
		Programs: programs,
		Hostname: system.Hostname,
		Platform: pname,
		Status:   state,
	}

	return machine
}

// LookForIP screen scrapes the IP from a virtual machine output mainly used for bridge/hosted machines
func LookForIP(l *logger.Logger, routes []virtualizers.NetworkInterface) []virtualizers.NetworkInterface {

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
	if len(ips) > 0 {
		for i, route := range routes {
			for j, port := range route.HTTP {
				routes[i].HTTP[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
			}
			for j, port := range route.HTTPS {
				routes[i].HTTPS[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
			}
			for j, port := range route.TCP {
				routes[i].TCP[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
			}
			for j, port := range route.UDP {
				routes[i].UDP[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
			}
		}
	}
	return routes
}

// generateRoutes ...
func generateRoutes(nics []vcfg.NetworkInterface) virtualizers.Routes {
	routes := virtualizers.Routes{}

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
	return routes
}

func fetchAddress(i int, routes virtualizers.Routes, port string, networkType virtualizers.NetworkProtocol) string {
	var addr string
	if len(routes.NIC) > i {
		nic := routes.NIC[i]
		if proto, ok := nic.Protocol[networkType]; ok {
			if pmap, ok := proto.Port[port]; ok {
				addr = pmap.Address
			}
		}
	}
	return addr
}

// Routes generates api friendly routes for the machine
func Routes(networks []vcfg.NetworkInterface) []virtualizers.NetworkInterface {
	var nics = networks
	routes := generateRoutes(nics)

	apiNics := make([]virtualizers.NetworkInterface, 0)
	for i, net := range networks {
		newNetwork := virtualizers.NetworkInterface{
			Name:    "",
			IP:      net.IP,
			Mask:    net.Mask,
			Gateway: net.Gateway,
		}
		for _, port := range net.UDP {
			newNetwork.UDP = append(newNetwork.UDP, virtualizers.RouteMap{
				Port:    port,
				Address: fetchAddress(i, routes, port, "udp"),
			})
		}
		for _, port := range net.TCP {
			newNetwork.TCP = append(newNetwork.TCP, virtualizers.RouteMap{
				Port:    port,
				Address: fetchAddress(i, routes, port, "tcp"),
			})
		}
		for _, port := range net.HTTP {
			newNetwork.HTTP = append(newNetwork.HTTP, virtualizers.RouteMap{
				Port:    port,
				Address: fetchAddress(i, routes, port, "http"),
			})
		}
		for _, port := range net.HTTPS {
			newNetwork.HTTPS = append(newNetwork.HTTPS, virtualizers.RouteMap{
				Port:    port,
				Address: fetchAddress(i, routes, port, "https"),
			})
		}
		apiNics = append(apiNics, newNetwork)
	}
	return apiNics
}
