// +build linux darwin

package virtualbox

import (
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"time"
)

// initLogging setup and read from the socket wait till the socker appears
// time out send the vm to 'broken' state
func (v *Virtualizer) initLogging() error {
	v.virtLogger.Write([]byte(fmt.Sprintf("initializing logger...\n")))

	count := 0
	for {
		if count == 15 {
			v.virtLogger.Write([]byte(fmt.Sprintf("error: %s\n", "unable to listen on unix socket within timeframe.")))
			return fmt.Errorf("unable to listen on unix socket within timeframe.")
		}
		conn, err := net.Dial("unix", filepath.ToSlash(filepath.Join(v.folder, "monitor.sock")))
		if err != nil {
			if !strings.Contains(err.Error(), "no such file or directory") {
				v.virtLogger.Write([]byte(fmt.Sprintf("error: %v\n", err)))
				return err
			}
		} else {
			v.sock = conn
			go io.Copy(v.serialLogger, conn)

			break
		}
		count++
		time.Sleep(time.Second * 1)
	}

	return nil
}
