package vcfg

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/vorteil/vorteil/pkg/elog"
)

// WithDefaults sets default values for certain fields
// if they are not set
func WithDefaults(v *VCFG, logger elog.View) error {

	if v.Networks == nil {
		v.Networks = make([]NetworkInterface, 0)
	}
	if len(v.Networks) == 0 {
		logger.Debugf("Creating default NIC with IP = dhcp")
		v.Networks = append(v.Networks, NetworkInterface{
			IP: "dhcp",
		})
	}

	if v.VM.RAM.String() == "" {
		logger.Debugf("Using default ram. RAM (128 MiB)")
		nBytes := 128 * 1024 * 1024
		v.VM.RAM = Bytes(nBytes)
	}

	if v.VM.CPUs == 0 {
		logger.Debugf("Using default no. CPUs (1)")
		v.VM.CPUs = 1
	}

	if v.System.Hostname == "" {
		if v.Info.Name != "" {
			v.System.Hostname = fmt.Sprintf("%s-$SALT", sanitizeHostname(v.Info.Name))
		} else {
			v.System.Hostname = "vorteil-$SALT"
		}
		logger.Debugf("Setting empty hostname field to '%s'", v.System.Hostname)
	}

	return nil
}

var hostnameRegexp = regexp.MustCompile(`(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])`)

func sanitizeHostname(s string) string {
	x := hostnameRegexp.FindAllString(s, -1)
	if x == nil {
		return "vorteil"
	}
	return strings.Trim(strings.Join(x, ""), "-")
}
