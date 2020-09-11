// +build windows

package kvm

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"runtime"
	"time"

	"code.vorteil.io/toolkit/cli/pkg/vm"
	"code.vorteil.io/toolkit/cli/pkg/vm/hype"
	"github.com/natefinch/npipe"
)

// Start ...
func (kvm *KVM) Start(ctx context.Context) error {

	err := kvm.VirtualMachine.Start(ctx)
	if err != nil {
		return err
	}

	switch kvm.State {
	case vm.Stopped:
		fallthrough
	case vm.Ready:
		// Start a ready VM
		kvm.UpdateState(vm.Starting)

		a := kvm.CommandArgs

		netArgs, err := kvm.initNetworkCards()
		if err != nil {
			kvm.Error("Error initilising network cards: %v", err)
		} else {
			a = append(kvm.CommandArgs, netArgs...)
		}

		exe, _ := hype.GetExecutable(hype.Hypervisors[hype.KVM])
		kvm.Command = exec.Command(exe, a...)

		err = kvm.initLogging()
		if err != nil {
			kvm.Error("Error initilising logging: %v", err)
			return err
		}

		startChan := make(chan error)

		kvm.LogStartArgs(ctx, "Starting %s with args: %s", hype.Hypervisors[hype.KVM], kvm.Command.Args)

		go func() {
			err = kvm.Command.Start()
			if err != nil {
				kvm.Error("Error starting VM: %v", err)
				err = kvm.ForceStop(ctx)
				if err != nil {
					kvm.Error("Error force stopping vm: %v", err)
				}
				close(startChan)
				return
			}

			if runtime.GOOS == "windows" {
				conn, err := npipe.Dial(fmt.Sprintf("\\\\.\\pipe\\%s", kvm.winPipe))
				if err != nil {
					kvm.Error("Error opening control pipe: %v", err)
					kvm.UpdateState(vm.Broken)
					return
				}
				kvm.sock = conn
				go io.Copy(ioutil.Discard, conn)
			} else {
				// wait for the unix socket to be created. If it isn't created in the time specified it will throw an error when it tries to dial
				for i := 0; i < 200; i++ {
					_, err := os.Stat(kvm.TempDir + "/monitor.sock")
					if err == nil {
						break
					}
					time.Sleep(time.Duration(i*i*35) * time.Microsecond)
				}

				// connect to the unix socket for shutdown/pause requests
				kvm.sock, err = net.Dial("unix", kvm.TempDir+"/monitor.sock")
				if err != nil {
					kvm.Error("Error opening socket: %v", err)
					kvm.UpdateState(vm.Broken)
					return
				}
			}

			kvm.UpdateState(vm.Alive)
			close(startChan)
			_, err = kvm.Command.Process.Wait()
			if err == nil || err.Error() != fmt.Errorf("wait: no child processes").Error() {
				if err != nil {
					kvm.Error("Error running VM: %v", err)
				}
				if kvm.State == vm.Alive {
					err = kvm.Stop(ctx)
					if err != nil {
						kvm.Error("Error stopping VM: %v", err)
					}
				}
			}
		}()

		<-startChan

	case vm.Paused:
		kvm.UpdateState(vm.Unpausing)

		// send continue request to QEMU
		if kvm.sock != nil {
			_, err = kvm.sock.Write([]byte("cont\n"))
			if err != nil {
				kvm.Error("Error writing continue request: %v", err)
				return err
			}

			// kvm.sock.Close()
		}
		// Send a continue signal to a "paused" (The proccess has been stopped) VM.
		// err = kvm.Command.Process.Signal(syscall.SIGCONT)
		// if err != nil {
		// 	kvm.Error("Error starting VM: %v", err)
		// 	kvm.UpdateState(vm.Stopped)
		// 	return err
		// }
		kvm.UpdateState(vm.Alive)

	default:
		return fmt.Errorf("cannot start vm in state '%s'", vm.Statuses[kvm.State])
	}
	return nil
}
