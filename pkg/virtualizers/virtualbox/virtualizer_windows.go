// +build windows

package virtualbox

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"
	"io"

	"github.com/natefinch/npipe"
)

// initLogging setup the pipe to write serial out from the app.
func (v *Virtualizer) initLogging() error {
	v.logger.Debugf("Initializing Logger...")
	conn, err := npipe.Dial(fmt.Sprintf("\\\\.\\pipe\\%s", v.id))
	if err != nil {
		v.logger.Errorf("Error dialing into pipe: %s", err.Error())
		return err
	}
	v.sock = conn
	go io.Copy(v.serialLogger, conn)

	return nil
}
