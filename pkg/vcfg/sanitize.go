package vcfg

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"errors"
	"fmt"

	"github.com/mattn/go-shellwords"
)

// hostname magic

func (p *Program) ProgramArgs() ([]string, error) {

	if p.Binary == "" && p.Args == "" {
		return nil, errors.New("no program binary or args defined")
	}

	s := []string{}
	if p.Binary != "" {
		s = append(s, p.Binary)
	}

	if p.Args != "" {
		sw := shellwords.NewParser()
		sw.ParseBacktick = false
		sw.ParseEnv = false
		args, err := sw.Parse(string(p.Args))
		if err != nil {
			return nil, fmt.Errorf("error parsing arg: %v", err)
		}
		s = append(s, args...)
	}

	return s, nil

}

func (vcfg *VCFG) SaltedHostname() string {
	hostname := vcfg.System.Hostname
	if hostname == "" {
		// use application name as base of hostname
		hostname = vcfg.Info.Name
		if hostname == "" {
			hostname = "vorteil"
		}

		// add $SALT so vinitd will add randomly generated characters on the end
		// 9 chars will be added by vinitd
		if len(hostname) > 54 {
			hostname = hostname[:54]
		}

		hostname = fmt.Sprintf("%s-$SALT", hostname)
	}

	l := 63
	if len(hostname) <= 64 {
		l = len(hostname)
	}

	return hostname[:l]
	// TODO: strip illegal characters
}
