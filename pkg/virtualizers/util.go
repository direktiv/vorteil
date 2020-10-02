package virtualizers

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// HostDevices is a virtualbox only function which returns a list of available host devices
func HostDevices() ([]string, error) {
	var hostDevices []string
	virtualizers, err := Backends()
	if err != nil {
		return nil, err
	}

	vboxInstalled := false
	for _, v := range virtualizers {
		if v == "virtualbox" {
			vboxInstalled = true
		}
	}

	if !vboxInstalled {
		return nil, fmt.Errorf("virtualbox must be installed to run this function")
	}

	cmd := exec.Command("VBoxManage", "list", "hostonlyifs")
	b, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(fmt.Sprintf("%s", b), "\n")
	for _, line := range lines {
		if strings.Contains(line, "Name:") {
			if strings.TrimSpace(strings.Split(strings.TrimPrefix(line, "Name:"), "VBoxNetworkName:")[0]) != "" {
				hostDevices = append(hostDevices, strings.TrimSpace(strings.Split(strings.TrimPrefix(line, "Name:"), "VBoxNetworkName:")[0]))

			}
		}
	}
	return hostDevices, nil
}

// VSwitches is a windows only function which returns the virtual switches hyper-v responds with
func VSwitches() ([]string, error) {
	virtualizers, err := Backends()
	if err != nil {
		return nil, err
	}

	hyperVInstalled := false
	for _, v := range virtualizers {
		if v == "hyperv" {
			hyperVInstalled = true
		}
	}
	if !hyperVInstalled {
		return nil, fmt.Errorf("hyperv must be available to run this function")
	}

	cmd := exec.Command(Powershell, "Get-VMSwitch", "|", "Select", "Name")
	b, err := cmd.CombinedOutput()
	if err != nil {
		if !strings.Contains(err.Error(), "exit status 1") {
			return nil, err
		} else {
			return nil, fmt.Errorf("%s", string(b))
		}
	}

	lines := strings.Split(fmt.Sprintf("%s", b), "----")
	if len(lines) >= 2 {
		split := strings.Split(strings.TrimSpace(lines[1]), "\n")
		return split, nil
	}

	return nil, fmt.Errorf("no virtual switches are created in the hyperv manager")

}

// BridgedDevices is a virtualbox only function which returns an array of available bridged devices
func BridgedDevices() ([]string, error) {
	var bridgedDevices []string
	virtualizers, err := Backends()
	if err != nil {
		return nil, err
	}
	vboxInstalled := false
	for _, v := range virtualizers {
		if v == "virtualbox" {
			vboxInstalled = true
		}
	}
	if !vboxInstalled {
		return nil, fmt.Errorf("virtualbox must be installed to run this function")
	}
	cmd := exec.Command("VBoxManage", "list", "bridgedifs")
	b, err := cmd.CombinedOutput()
	if err != nil {
		return nil, err
	}

	check := make(map[string]int)
	lines := strings.Split(fmt.Sprintf("%s", b), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Name:") {
			device := strings.TrimSpace(strings.Split(line, "Name:")[1])
			check[device] = 1
		}
	}
	for device := range check {
		bridgedDevices = append(bridgedDevices, device)
	}

	return bridgedDevices, nil
}

// CheckNameExistsVirtualBox checks the virtualbox list to see if a vm with the same name
// has already been created
func CheckNameExistsVirtualBox(name string) (bool, error) {

	command := exec.Command("VBoxManage", "showvminfo", name)
	var outB, errB bytes.Buffer
	command.Stdout = &outB
	command.Stderr = &errB

	err := command.Run()
	if err != nil {
		errMsg := strings.Split(fmt.Sprintf("%s", command.Stderr), "\n")[0]
		if errMsg == fmt.Sprintf("VBoxManage: error: Could not find a registered machine named '%s'", name) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// CheckNameExistsHyperV checks the hyperv driver to see if a vm with the same name already exists
func CheckNameExistsHyperV(name string) (bool, error) {
	command := exec.Command(Powershell, "Get-VM", "|", "Select", "Name")
	var outB, errB bytes.Buffer
	command.Stdout = &outB
	command.Stderr = &errB
	err := command.Run()
	if err != nil {
		return false, err
	}
	list := strings.Split(strings.TrimSpace(outB.String()), "----")
	if len(list) > 1 {
		vmlist := strings.Split(strings.TrimSpace(list[1]), "\n")
		for _, vm := range vmlist {
			if vm == name {
				return true, nil
			}
		}
	}

	return false, nil
}

// BindPort attempts to bind ports and if not available will assign a different port.
func BindPort(netType, protocol, port string) (string, string, error) {
	var (
		bind     string
		netRoute string
		isBound  bool
	)

	if netType == "nat" {
		netRoute = fmt.Sprintf("localhost:%s", port)
		// log attempting to bind
		switch protocol {
		case "udp":
			addr, err := net.ResolveUDPAddr("udp4", netRoute)
			if err != nil {
				return "", netRoute, err
			}
			listener, err := net.ListenUDP("udp4", addr)
			if err == nil {
				s := strings.Split(listener.LocalAddr().String(), ":")
				bind = s[len(s)-1]
				isBound = true
				listener.Close()
			}
		default:
			listener, err := net.Listen("tcp4", fmt.Sprintf(":%s", port))
			if err == nil {
				s := strings.Split(listener.Addr().String(), ":")
				bind = s[len(s)-1]
				isBound = true
				listener.Close()
			}
		}
		if !isBound {
			// log that it failed to bind netRoute
			netRoute = "localhost:0"
			switch protocol {
			case "udp":
				addr, err := net.ResolveUDPAddr("udp4", netRoute)
				if err != nil {
					return "", netRoute, err
				}
				listener, err := net.ListenUDP("udp4", addr)
				if err == nil {
					s := strings.Split(listener.LocalAddr().String(), ":")
					bind = s[len(s)-1]
					isBound = true
					netRoute = "localhost:" + bind
					listener.Close()
				} else {
					return "", netRoute, err
				}
			default:
				listener, err := net.Listen("tcp4", netRoute)
				if err == nil {
					s := strings.Split(listener.Addr().String(), ":")
					bind = s[len(s)-1]
					isBound = true
					netRoute = "localhost:" + bind
					listener.Close()
				} else {
					return "", netRoute, err
				}
			}
		}
	}
	// Bound on address netRoute
	return bind, netRoute, nil
}

// GetExecutable returns the name of the executable for the virtualizer.
func GetExecutable(virtualizer string) (string, error) {
	switch virtualizer {
	case "qemu":
		return "qemu-system-x86_64", nil
	case "virtualbox":
		return "VBoxManage", nil
	case "vmware":
		return "vmrun", nil
	case "firecracker":
		return "firecracker", nil
	case "hyperv":
		return Powershell, nil
	default:
		return "", fmt.Errorf("%s is not supported", virtualizer)
	}
}

// Backends returns the currently available hypervisors the system running on.
func Backends() ([]string, error) {
	var installedVirtualizers []string
	path := os.Getenv("PATH")
	separated := ":"
	if runtime.GOOS == "windows" {
		separated = ";"
	}
	if !strings.Contains(path, vbox) {
		err := os.Setenv("PATH", fmt.Sprintf("%s%s%s", path, separated, vbox))
		if err != nil {
			return nil, err
		}
	}
	path = os.Getenv("PATH")

	// if !strings.Contains(path, vmware) {
	// 	err := os.Setenv("PATH", fmt.Sprintf("%s%s%s", path, separated, vmware))
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// }
	// path = os.Getenv("PATH")

	if !strings.Contains(path, qemu) {
		err := os.Setenv("PATH", fmt.Sprintf("%s%s%s", path, separated, qemu))
		if err != nil {
			return nil, err
		}
	}

	path = os.Getenv("PATH")

	if runtime.GOOS == "linux" {
		if !strings.Contains(path, firecracker) {
			err := os.Setenv("PATH", fmt.Sprintf("%s%s%s", path, separated, firecracker))
			if err != nil {
				return nil, err
			}
		}
		path = os.Getenv("PATH")
	}

	paths := filepath.SplitList(path)

	for _, v := range supportedVirtualizers {
		if v == "hyperv" && runtime.GOOS != "windows" {
			continue
		} else {
			virt, err := GetExecutable(v)
			if err != nil {
				break
			}
			if runtime.GOOS == "windows" {
				virt = virt + ".exe"
			}
			for _, p := range paths {
				p := filepath.Join(p, virt)
				_, err = os.Stat(p)
				if err == nil {
					found := false
					for _, virts := range installedVirtualizers {
						if virts == v {
							found = true
						}
					}
					if !found {
						installedVirtualizers = append(installedVirtualizers, v)
					}
				}
			}
		}

	}

	// If we're on windows check to see if hyperv by checking if the ethernet adapter is online
	if runtime.GOOS == "windows" {
		cmd := exec.Command("ipconfig", "/all")
		// cmd := exec.Command(Powershell, "Get-WindowsOptionalFeature", "-FeatureName", "Microsoft-Hyper-V-All", "-Online")
		resp, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Printf("error checking for hyperv: %v\n", err)
		}
		output := string(resp)

		if strings.Contains(output, "Hyper-V Virtual Ethernet Adapter") {
			installedVirtualizers = append(installedVirtualizers, "hyperv")
		}
	}
	return installedVirtualizers, nil
}
