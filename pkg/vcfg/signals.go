package vcfg

import (
	"fmt"
	"syscall"
)

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

//TerminateSignal : The Signal to send to a program on termination
//	Additional information can be found @ https://support.vorteil.io/docs/VCFG-Reference/program/terminate
type TerminateSignal string

// TerminateSignals : Supported Signals
var TerminateSignals = map[TerminateSignal]syscall.Signal{
	"SIGINT":  syscall.SIGINT,  // Term    Interrupt from keyboard
	"SIGKILL": syscall.SIGKILL, // Term    Kill signal
	"SIGPWR":  syscall.SIGPWR,  // Term    Power failure (System V)
	"SIGQUIT": syscall.SIGQUIT, // Core    Quit from keyboard
	"SIGSTOP": syscall.SIGSTOP, // Stop    Stop process
	"SIGTERM": syscall.SIGTERM, // Term    Termination signal
}

// Validate TerminateSignal
func (tSig *TerminateSignal) Validate() (err error) {
	validSignals := ""
	i := 0
	for strSig := range TerminateSignals {
		validSignals += string(strSig)

		if i < len(TerminateSignals)-1 {
			validSignals += ", "
		}

		if *tSig == strSig {
			return
		}

		i++
	}

	return fmt.Errorf("terminate signal '%s' is not supported. Supported Signals: %s", *tSig, validSignals)
}

// Signal : Return syscall Signal
func (tSig *TerminateSignal) Signal() (syscall.Signal, error) {
	if err := tSig.Validate(); err != nil {
		return syscall.Signal(0), err
	}

	return TerminateSignals[*tSig], nil
}
