// +build linux darwin

package hyperv

import "errors"

// initLogs returns an error as this is a windows only hypervisor
func (v *Virtualizer) initLogs() error {
	return errors.New("hyperv is not implemented on this operating system.. how did you get here?")
}
