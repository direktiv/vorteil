// +build linux windows

package kvm

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"time"

	"code.vorteil.io/toolkit/cli/pkg/util/file"

	"code.vorteil.io/toolkit/cli/pkg/vm"
	"code.vorteil.io/toolkit/cli/pkg/vm/shared"
	shellwords "github.com/mattn/go-shellwords"
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

var osFlags = "-cpu host -enable-kvm"

func init() {
	if runtime.GOOS == "windows" {
		osFlags = "-accel whpx"
	}
}

// Stop ...
func (kvm *KVM) Stop(ctx context.Context) error {

	err := kvm.VirtualMachine.Stop(ctx)
	if err != nil {
		return err
	}

	defer kvm.postStop(ctx)

	// stop kvm
	err = kvm.stopKVM(ctx)
	if err != nil {
		return err
	}

	return nil
}

// ForceStop ...
func (kvm *KVM) ForceStop(ctx context.Context) error {
	err := kvm.VirtualMachine.ForceStop(ctx)
	if err != nil && !shared.IsBadState(err) {
		return err
	}

	defer kvm.postStop(ctx)

	// stop kvm
	err = kvm.stopKVM(ctx)
	if err != nil {
		return err
	}

	// post stop clean up

	return nil
}

func (kvm *KVM) stopKVM(ctx context.Context) error {

	if kvm.sock != nil {
		defer kvm.sock.Close()

		// send powerdown request to QEMU
		_, err := kvm.sock.Write([]byte("system_powerdown\n"))
		if err != nil && err.Error() != fmt.Errorf("write unix @->%s/monitor.sock: write: broken pipe", kvm.TempDir).Error() {
			kvm.Error("Error writing shutdown request: %v", err)
			return err
		}
		time.Sleep(time.Second * 5)

		// wait for the vm to shut down. There isn't a clean way of querying if it's still running or not
		// time.Sleep(30 * time.Second)
	}

	// Kill the proccess executing the VM if it is still running
	if kvm.Command != nil && kvm.Command.Process != nil {
		err := kvm.Command.Process.Kill()
		if err == nil || err.Error() != fmt.Errorf("os: process already finished").Error() {
			if err != nil {
				kvm.Debug("Debug killing VM: %v", err)
				return nil
			}
			// Wait for the proccess to have been terminated
			_, err = kvm.Command.Process.Wait()
			if err != nil && !(err.Error() == fmt.Errorf("wait: no child processes").Error() || err.Error() == fmt.Errorf("waitid: no child processes").Error()) {
				kvm.Error("Error occured while waiting for VM to die: %v", err)
				return err
			}
		}
	}

	return nil
}

func (kvm *KVM) postStop(ctx context.Context) {

	// remove the monitor socket
	if kvm.sock != nil {
		os.Remove(kvm.TempDir + "/monitor.sock")
	}

	// clean up the old logs and reinitilise them
	if kvm.errPipe != nil {
		kvm.errPipe.Close()
	}
	if kvm.outPipe != nil {
		kvm.outPipe.Close()
	}

	kvm.LogsList.Init()

	kvm.UpdateState(vm.Stopped)

}

// Pause ...
func (kvm *KVM) Pause(ctx context.Context) error {

	err := kvm.VirtualMachine.Pause(ctx)
	if err != nil {
		return err
	}

	if kvm.sock != nil {
		// Send stop request to QEMU
		_, err := kvm.sock.Write([]byte("stop\n"))
		if err != nil {
			kvm.Error("Error writing stop request: %v", err)
			return err
		}

		// kvm.sock.Close()
	}

	// Send a stop signal to "pause" the VM
	// err = kvm.Command.Process.Signal(syscall.SIGSTOP)
	// if err != nil {
	// 	kvm.Error("Error pausing VM: %v", err)
	// 	return err
	// }

	kvm.UpdateState(vm.Paused)
	return nil
}

// Delete ...
func (kvm *KVM) Delete(ctx context.Context) error {

	err := kvm.VirtualMachine.Delete(ctx)
	if err != nil {
		return err
	}

	if kvm.State != vm.Stopped {
		// Stop VM before deleting.
		err = kvm.ForceStop(ctx)
		if err != nil {
			return err
		}
	}

	kvm.UpdateState(vm.Deleting)

	// close the pipes and channels used for logging
	if kvm.errPipe != nil {
		kvm.errPipe.Close()
	}
	if kvm.outPipe != nil {
		kvm.outPipe.Close()
	}
	if kvm.logIDCount != nil {
		close(kvm.logIDCount)
	}

	l := kvm.LogStreamersList.Front()
	for i := 0; i < kvm.LogStreamersList.Len(); i++ {
		l.Value.(*shared.LogStreamerBase).SetError(fmt.Errorf("Virtual Box Stopped")) // Set the error message for LogStreamers listening to this VM
		l.Value.(vm.LogStreamer).Close()
		l = l.Next()
	}
	kvm.LogStreamersList.Init() // clears the list

	if !kvm.Persist {
		os.RemoveAll(kvm.TempDir)
		if err != nil {
			kvm.Error("Error removing temporary directory: %v", err)
		}
	}

	kvm.UpdateState(vm.Deleted)

	kvm.StateList.Init() // clears the status subscriber list
	kvm.RemoveVirtualMachine()

	return nil
}

// Detach ...
func (kvm *KVM) Detach(ctx context.Context) error {

	return fmt.Errorf("KVM does not support detaching")

	// err := kvm.VirtualMachine.Detach(ctx)
	// if err != nil {
	// 	return err
	// }

	// kvm.UpdateState(vm.Deleting)

	// // close the pipes and channels used for logging
	// if kvm.errPipe != nil {
	// 	kvm.errPipe.Close()
	// }
	// if kvm.outPipe != nil {
	// 	kvm.outPipe.Close()
	// }
	// if kvm.logIDCount != nil {
	// 	close(kvm.logIDCount)
	// }

	// l := kvm.LogStreamersList.Front()
	// for i := 0; i < kvm.LogStreamersList.Len(); i++ {
	// 	l.Value.(*shared.LogStreamerBase).SetError(fmt.Errorf("Virtual Box Stopped")) // Set the error message for LogStreamers listening to this VM
	// 	l.Value.(vm.LogStreamer).Close()
	// 	l = l.Next()
	// }
	// kvm.LogStreamersList.Init() // clears the list

	// if !kvm.Persist {
	// 	os.RemoveAll(kvm.TempDir)
	// 	if err != nil {
	// 		kvm.Error("Error removing temporary directory: %v", err)
	// 	}
	// }

	// kvm.UpdateState(vm.Deleted)

	// kvm.StateList.Init() // clears the status subscriber list
	// kvm.RemoveVirtualMachine()

	// return nil
}

// DownloadImage ...
func (kvm *KVM) DownloadImage() (file.File, error) {

	if !(kvm.State == vm.Stopped || kvm.State == vm.Ready) {
		return nil, &shared.VMError{VMErrorType: shared.BadState, Message: fmt.Sprintf("Cannot download disk image in state '%s'. Must be in %s or %s", vm.Statuses[kvm.State], vm.Statuses[vm.Stopped], vm.Statuses[vm.Ready])}
	}

	f, err := file.LazyOpen(kvm.TempDisk)
	if err != nil {
		kvm.Error("Error downloading disk image: %v", err)
		return nil, err
	}

	return f, nil
}

// Initilize logging for the vm
func (kvm *KVM) initLogging() error {

	kvm.VerboseDebug("Initilising logging")

	// logIDCount is used as a sempaphore
	kvm.logIDCount = make(chan uint, 1)
	kvm.logIDCount <- 0

	// capture stderr and stdout from the vm and store them as Logs
	errPipe, err := kvm.Command.StderrPipe()
	if err != nil {
		return err
	}
	se := bufio.NewReader(errPipe)

	outPipe, err := kvm.Command.StdoutPipe()
	if err != nil {
		return err
	}
	so := bufio.NewReader(outPipe)

	kvm.errPipe = errPipe
	kvm.outPipe = outPipe

	go kvm.readPipe(se, true)
	go kvm.readPipe(so, false)

	return nil
}

// go routine to read from stdout/stderr from the VM and push the logs to listening channels.
func (kvm *KVM) readPipe(pipe *bufio.Reader, isStdErr bool) {

	if pipe == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			// This recovers from a pacnic which can be caused if the logIDCount channel is closed
			// between the read and write operations. We don't need to do anything here besides recover
			// as the pipes are now closed.
		}
	}()
	for {

		b, err := pipe.ReadString('\n')
		if err != nil {
			return
		}

		b = b[:len(b)-1]

		if isStdErr {
			kvm.Error("%v", string(b))
		}

		id, ok := <-kvm.logIDCount // request logIDCount
		if !ok {
			return // channel has closed, exit
		}
		// create Log
		log := &shared.LogMessage{
			Identifier: id,
			LogMessage: string(b),
		}
		id++
		kvm.logIDCount <- id // release logIDCount

		if kvm.LogsList.Len() > 4096 {
			// Keep the log list to a reasonable number of logs.
			kvm.LogsList.Remove(kvm.LogsList.Front())
		}
		kvm.LogsList.PushBack(log)

		// Send the log to any LogStreamers listening
		l := kvm.LogStreamersList.Front()
		for i := 0; i < kvm.LogStreamersList.Len(); i++ {
			l.Value.(*shared.LogStreamerBase).Outbox() <- log
			l = l.Next()
		}
	}
}

func (kvm *KVM) initNetworkCards() ([]string, error) {

	kvm.VerboseDebug("Initilising network cards...")

	var str string
	noNic := 0
	hasDefinedPorts := false

	// iterate through the network cards. There can be up to 4
	for i := 0; i < len(kvm.Routes().NIC); i++ {
		var card string
		if len(kvm.Routes().NIC[i].Protocol) <= 0 {
			continue
		}
		noNic++

		if kvm.NetworkType == shared.Bridged {
			str += fmt.Sprintf(" -netdev bridge,id=network%d,br=%s -device virtio-net-pci,netdev=network%d,id=virtio%d,mac=26:10:05:00:00:0%x", i, kvm.NetworkName, i, i, 0xa+(i*0x1))
		} else {
			// card += fmt.Sprintf(",dhcpstart=10.0.2.%v", 16+(i*16))
			// protocols for each network card
			for np, ports := range kvm.Routes().NIC[i].Protocol {
				// ports for each protocol
				for port := range ports.Port {

					bind, nr, err := kvm.BindPort(np, port)
					if err != nil {
						return nil, err
					}

					hasDefinedPorts = true
					ports.Port[port] = nr
					protocol := "tcp"
					if string(np) == "udp" {
						protocol = "udp"
					}
					card += fmt.Sprintf(",hostfwd=%s::%s-:%s", protocol, bind, port)
				}
			}

			// double check virtio is working. it was broken and we had to use e1000 - 09/07/18
			str += fmt.Sprintf(" -netdev user,id=network%v%s -device virtio-net-pci,netdev=network%v,id=virtio%v,mac=26:10:05:00:00:0%x", i, card, i, i, 0xa+(i*0x1))
			// str += fmt.Sprintf(" -netdev user,id=network%v%s -device e1000,netdev=network%v,id=virtio%v,mac=26:10:05:00:00:0%x", i, card, i, i, 0xa+(i*0x1))
		}
	}
	if noNic > 0 && !hasDefinedPorts {
		kvm.Warn("Warning: VM has network cards but no defined ports")
	}

	kvm.StateUpdated()

	kvm.Debug("Network args:%s", str)

	return shellwords.Parse(str)
}
