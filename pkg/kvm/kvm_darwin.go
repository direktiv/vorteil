// +build darwin

package kvm

import (
	"io"
	"net"

	"code.vorteil.io/toolkit/cli/pkg/vm/shared"
)

// KVM ...
type KVM struct {
	shared.VirtualMachine
	CommandArgs []string
	errPipe     io.ReadCloser // Stderr for this VM
	outPipe     io.ReadCloser // Stdout for this VM
	logIDCount  chan uint     // channel used as a semephore so that the IDCount isn't caught in a race condition
	NetworkType shared.NetworkType
	NetworkName string
	winPipe     string
	sock        net.Conn
}

func (kvm *KVM) initLogging() error {
	return nil
}

var osFlags = "-cpu host -enable-kvm"
