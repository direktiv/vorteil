// +build windows

package qemu

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/mattn/go-shellwords"
	"github.com/natefinch/npipe"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/virtualizers"
)

// Start creates the virtualmachine and runs it
func (v *Virtualizer) Start() error {
	v.logger.Debugf("Starting VM")
	v.command = exec.Command(v.command.Args[0], v.command.Args[1:]...)
	v.command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	switch v.State() {
	case "ready":
		v.state = virtualizers.Changing

		err := v.initLogging()
		if err != nil {
			return err
		}

		go func() {
			err = v.command.Start()
			if err != nil {
				v.logger.Errorf("Error executing Start: %s", err.Error())
			}
			conn, err := npipe.Dial(fmt.Sprintf("\\\\.\\pipe\\%s", v.id))
			if err != nil {
				v.logger.Errorf("Error dialing pipe: %s", err.Error())
			}
			v.sock = conn
			go io.Copy(ioutil.Discard, conn)
			v.state = virtualizers.Alive

			_, err = v.command.Process.Wait()
			if err == nil || err.Error() != fmt.Errorf("wait: no child processes").Error() {
				if err != nil {
					v.logger.Errorf("Error Command Wait: %s", err.Error())
				}
			}

			v.state = virtualizers.Ready

			if v.sock != nil {
				v.sock.Close()

				// vm should be stopped by now so close the pipes
				v.errPipe.Close()
				v.outPipe.Close()
				v.disk.Close()
			}

		}()

	default:
		return fmt.Errorf("vm not in a state to be started currently in: %s", v.State())
	}
	return nil
}

// prepare sets the fields and arguments to spawn the virtual machine
func (o *operation) prepare(args *virtualizers.PrepareArgs) {
	var returnErr error
	progress := o.logger.NewProgress("Preparing QEMU machine", "", 0)
	defer progress.Finish(false)
	o.updateStatus(fmt.Sprintf("Building qemu command..."))
	defer func() {
		o.finished(returnErr)
	}()

	executable, err := virtualizers.GetExecutable(VirtualizerID)
	if err != nil {
		returnErr = err
		return
	}

	o.networkType = "nat"
	o.state = "initializing"
	o.name = args.Name
	o.folder = filepath.Dir(args.ImagePath)
	o.id = strings.Split(filepath.Base(o.folder), "-")[1]

	diskpath := filepath.ToSlash(args.ImagePath)
	diskformat := "raw"

	argsCommand := createArgs(o.config.VM.CPUs, o.config.VM.RAM.Units(vcfg.MiB), o.headless, diskpath, diskformat)
	argsCommand += fmt.Sprintf(" -monitor pipe:%s", o.id)

	params, err := shellwords.Parse(argsCommand)
	if err != nil {
		returnErr = err
		return
	}

	command := exec.Command(executable, params...)
	o.command = command

	netArgs, err := o.initializeNetworkCards()
	if err != nil {
		returnErr = err
	}
	o.command.Args = append(o.command.Args, netArgs...)

	o.logger.Infof("Creating QEMU VM with Args: %s", strings.Join(o.command.Args, " "))

	o.state = "ready"

	_, loaded := virtualizers.ActiveVMs.LoadOrStore(o.name, o.Virtualizer)
	if loaded {
		returnErr = errors.New("virtual machine already exists")
	}

	if args.Start {
		err = o.Start()
		if err != nil {
			returnErr = err
		}
	}

}

// GenerateScript generates a .bat file to be able to run qemu standalone
func (v *Virtualizer) GenerateScript(source string) error {
	var err error

	// look up full path to qemu
	v.command.Args[0], err = exec.LookPath(v.command.Args[0])
	if err != nil {
		return err
	}
	v.command.Args[0] = fmt.Sprintf("\"%s\"", v.command.Args[0])

	name := filepath.Base(v.folder)
	err = os.MkdirAll(filepath.Join(source), 0777)
	if err != nil {
		return err
	}
	// copy disk contents to source directory
	err = os.Rename(v.folder, filepath.Join(source, name))
	if err != nil {
		return err
	}

	args := strings.Join(v.command.Args, " ")
	// replace disk path
	args = strings.ReplaceAll(args, filepath.ToSlash(filepath.Join(v.folder, fmt.Sprintf("%s.raw", v.name))), fmt.Sprintf("\"%s\"", filepath.ToSlash(filepath.Join(source, name, fmt.Sprintf("%s.raw ", v.name)))))
	// replace monitor with nothing
	args = strings.ReplaceAll(args, fmt.Sprintf("-monitor pipe:%s", v.id), "")

	f, err := os.Create(filepath.Join(source, name, "start.bat"))
	if err != nil {
		return err
	}

	f.Write([]byte(args))

	defer f.Close()

	return nil
}
