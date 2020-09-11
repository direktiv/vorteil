// +build windows

package vmware

import (
	"fmt"
	"io"

	"github.com/natefinch/npipe"
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
	go v.lookForIP()

	return nil
}
