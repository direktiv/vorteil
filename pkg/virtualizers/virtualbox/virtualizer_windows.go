// +build windows

package virtualbox

import (
	"fmt"
	"io"

	"github.com/natefinch/npipe"
)

// initLogging setup the pipe to write serial out from the app.
func (v *Virtualizer) initLogging() error {
	v.virtLogger.Write([]byte(fmt.Sprintf("initializing logger...\n")))
	conn, err := npipe.Dial(fmt.Sprintf("\\\\.\\pipe\\%s", v.id))
	if err != nil {
		v.virtLogger.Write([]byte(fmt.Sprintf("error dialing into pipe: %v\n", err)))
		return err
	}
	v.sock = conn
	go io.Copy(v.serialLogger, conn)

	return nil
}
