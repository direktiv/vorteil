package vcfg

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
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

	if v.VM.CPUs == 0 {
		logger.Debugf("Using default no. CPUs (1)")
		v.VM.CPUs = 1
	}

	return nil
}
