// +build windows

package vmware

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
		ips := util.LookForIP(v.serialLogger)
		if len(ips) > 0 {
			for i, route := range v.routes {
				for j, port := range route.HTTP {
					v.routes[i].HTTP[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
				}
				for j, port := range route.HTTPS {
					v.routes[i].HTTPS[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
				}
				for j, port := range route.TCP {
					v.routes[i].TCP[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
				}
				for j, port := range route.UDP {
					v.routes[i].UDP[j].Address = fmt.Sprintf("%s:%s", ips[i], port.Port)
				}
			}
		}
	}()

	return nil
}
