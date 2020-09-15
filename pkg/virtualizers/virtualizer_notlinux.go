// +build windows darwin
package virtualizers

import "errors"

func SetupBridgeAndDHCPServer() error {
	return errors.New("firecracker init not supported on this operating system")
}
