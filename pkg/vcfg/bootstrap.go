package vcfg

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

type Command string

// Bootstrap commands
const (
	_                       Command = "BootstrapCommand"
	BootstrapWaitFile               = "WAIT_FILE"
	BootstrapWaitPort               = "WAIT_PORT"
	BootstrapSleep                  = "SLEEP"
	BootstrapFindAndReplace         = "FIND_AND_REPLACE"
)
