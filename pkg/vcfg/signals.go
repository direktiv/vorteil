package vcfg

import (
	"fmt"
)

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

//TerminateSignal : The Signal to send to a program on termination
//	Additional information can be found @ https://support.vorteil.io/docs/VCFG-Reference/program/terminate
type TerminateSignal string

// TerminateSignals : Supported Signals
var TerminateSignals = []TerminateSignal{
	"SIGINT",  // Term    Interrupt from keyboard
	"SIGKILL", // Term    Kill signal
	"SIGPWR",  // Term    Power failure (System V)
	"SIGQUIT", // Core    Quit from keyboard
	"SIGSTOP", // Stop    Stop process
	"SIGTERM", // Term    Termination signal
}

// Validate TerminateSignal
func (tSig *TerminateSignal) Validate() (err error) {
	validSignals := ""
	for i := range TerminateSignals {
		validSignals += string(TerminateSignals[i])
		if len(TerminateSignals)-1 != i {
			validSignals += ", "
		}

		if *tSig == TerminateSignals[i] {
			return
		}
	}

	return fmt.Errorf("terminate signal '%s' is not supported. Supported Signals: %s", *tSig, validSignals)
}
