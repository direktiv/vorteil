package vcfg

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import "github.com/imdario/mergo"

// Merge ..
func Merge(a, b *VCFG) (*VCFG, error) {

	var err error

	// Sysctl
	if a.Sysctl == nil {
		a.Sysctl = b.Sysctl
	} else if b.Sysctl != nil {
		a.Sysctl = mergeStringMap(a.Sysctl, b.Sysctl)
	}

	// programs, logging, and networks
	err = mergeProgramsLoggingNetworks(a, b)
	if err != nil {
		return nil, err
	}

	// system, info, and vm
	err = mergeSystemInfoVM(a, b)
	if err != nil {
		return nil, err
	}

	// nfs, and routes
	err = mergeNFSRoutes(a, b)
	if err != nil {
		return nil, err
	}

	return a, nil
}

func mergeProgramsLoggingNetworks(a, b *VCFG) error {
	// Programs
	if err := a.mergePrograms(b); err != nil {
		return err
	}

	// Logging
	if err := a.mergeLogging(b); err != nil {
		return err
	}

	// Networks
	if err := a.mergeNetworks(b); err != nil {
		return err
	}

	return nil
}

func mergeSystemInfoVM(a, b *VCFG) error {
	// System.DNS
	dns := mergeStringArrayExcludingDuplicateValues(a.System.DNS, b.System.DNS)

	// System.NTP
	ntp := mergeStringArrayExcludingDuplicateValues(a.System.NTP, b.System.NTP)

	// System
	err := mergo.Merge(&a.System, &b.System, mergo.WithOverride)
	if err != nil {
		return err
	}

	a.System.DNS = dns
	a.System.NTP = ntp

	// Info
	err = mergo.Merge(&a.Info, &b.Info)
	if err != nil {
		return err
	}

	// VM
	err = mergo.Merge(&a.VM, &b.VM, mergo.WithOverride)
	if err != nil {
		return err
	}

	return nil
}

func mergeNFSRoutes(a, b *VCFG) error {
	// NFS
	if err := a.mergeNFS(b); err != nil {
		return err
	}

	// Routes
	if err := a.mergeRoutes(b); err != nil {
		return err
	}

	return nil
}

func (vcfg *VCFG) mergePrograms(b *VCFG) error {
	if vcfg.Programs == nil {
		vcfg.Programs = b.Programs
	} else if b.Programs != nil {

		for k, p := range vcfg.Programs {
			if len(b.Programs) > k {

				// merge b.Programs[k] over p
				envs := mergeStringArray(p.Env, b.Programs[k].Env)
				bstp := mergeStringArray(p.Bootstrap, b.Programs[k].Bootstrap)
				logfiles := mergeStringArrayExcludingDuplicateValues(p.LogFiles, b.Programs[k].LogFiles)

				err := mergo.Merge(&p, &b.Programs[k], mergo.WithOverride)
				if err != nil {
					return err
				}

				p.Env = envs
				p.Bootstrap = bstp
				p.LogFiles = logfiles

				vcfg.Programs[k] = p

			}
		}

		if len(b.Programs) > len(vcfg.Programs) {
			vcfg.Programs = append(vcfg.Programs, b.Programs[len(vcfg.Programs):]...)
		}
	}

	return nil
}

func (vcfg *VCFG) mergeNetworks(b *VCFG) error {
	if vcfg.Networks == nil {
		vcfg.Networks = b.Networks
	} else if b.Networks != nil {

		for k, n := range vcfg.Networks {

			if len(b.Networks) > k {

				// merge b.Networks[k] over p
				http := mergeStringArrayExcludingDuplicateValues(n.HTTP, b.Networks[k].HTTP)
				https := mergeStringArrayExcludingDuplicateValues(n.HTTPS, b.Networks[k].HTTPS)
				udp := mergeStringArrayExcludingDuplicateValues(n.UDP, b.Networks[k].UDP)
				tcp := mergeStringArrayExcludingDuplicateValues(n.TCP, b.Networks[k].TCP)

				err := mergo.Merge(&n, &b.Networks[k], mergo.WithOverride)
				if err != nil {
					return err
				}

				n.HTTP = http
				n.HTTPS = https
				n.UDP = udp
				n.TCP = tcp

				vcfg.Networks[k] = n
			}

			if len(b.Networks) > len(vcfg.Networks) {
				vcfg.Networks = append(vcfg.Networks, b.Networks[len(vcfg.Networks):]...)
			}
		}

	}

	return nil
}

func (vcfg *VCFG) mergeRoutes(b *VCFG) error {
	if vcfg.Routing == nil {
		vcfg.Routing = b.Routing
	} else if b.Routing != nil {

		for k, r := range vcfg.Routing {
			if len(b.Routing) > k {
				err := mergo.Merge(&r, &b.Routing[k], mergo.WithOverride)
				if err != nil {
					return err
				}

				vcfg.Routing[k] = r
			}
		}

		if len(b.Routing) > len(vcfg.Routing) {
			vcfg.Routing = append(vcfg.Routing, b.Routing[len(vcfg.Routing):]...)
		}
	}

	return nil
}

func (vcfg *VCFG) mergeNFS(b *VCFG) error {
	if vcfg.NFS == nil {
		vcfg.NFS = b.NFS
	} else if b.NFS != nil {

		for k, n := range vcfg.NFS {
			if len(b.NFS) > k {
				err := mergo.Merge(&n, &b.NFS[k], mergo.WithOverride)
				if err != nil {
					return err
				}

				vcfg.NFS[k] = n
			}
		}

		if len(b.NFS) > len(vcfg.NFS) {
			vcfg.NFS = append(vcfg.NFS, b.NFS[len(vcfg.NFS):]...)
		}

	}

	return nil
}

func (vcfg *VCFG) mergeLogging(b *VCFG) error {
	if vcfg.Logging == nil {
		vcfg.Logging = b.Logging
	} else if b.Logging != nil {

		for k, p := range vcfg.Logging {
			if len(b.Logging) > k {
				cfgs := mergeStringArray(p.Config, b.Logging[k].Config)

				err := mergo.Merge(&p, &b.Logging[k], mergo.WithOverride)
				if err != nil {
					return err
				}

				p.Config = cfgs
				vcfg.Logging[k] = p
			}
		}

		if len(b.Logging) > len(vcfg.Logging) {
			vcfg.Logging = append(vcfg.Logging, b.Logging[len(vcfg.Logging):]...)
		}

		for k, r := range vcfg.Logging {
			err := mergo.Merge(&r, &b.Logging[k], mergo.WithOverride)
			if err != nil {
				return err
			}
		}

		if len(b.Logging) > len(vcfg.Logging) {
			vcfg.Logging = append(vcfg.Logging, b.Logging[len(vcfg.Logging):]...)
		}
	}

	return nil
}
