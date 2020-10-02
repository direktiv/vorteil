// +build windows

package vmware

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"
	"io"

	"github.com/natefinch/npipe"
	"github.com/vorteil/vorteil/pkg/virtualizers/util"
)

// initLogs sets the logger up and attaches to the socket
func (v *Virtualizer) initLogs() error {
	v.log("info", "Initializing Logger")
	conn, err := npipe.Dial(fmt.Sprintf("\\\\.\\pipe\\%s", v.id))
	if err != nil {
		v.log("error", "Error dialing pipe: %v", err)
	}
	v.sock = conn
	go io.Copy(v.serialLogger, conn)
	go func() {
		v.routes = util.LookForIP(v.serialLogger, v.routes)
	}()

	return nil
}
