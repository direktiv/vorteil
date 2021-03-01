// +build linux darwin

package qemu

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-shellwords"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/virtualizers"
)

// Start creates the virtualmachine and runs it
func (v *Virtualizer) Start() error {
	v.logger.Debugf("Starting VM")
	v.command = exec.Command(v.command.Args[0], v.command.Args[1:]...)
	v.command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
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
				v.logger.Errorf("Error Executing Start: %s", err.Error())
			}

			polling := true
			count := 0
			for {
				if !polling {
					break
				}
				if count == 10 {
					v.logger.Errorf("Error: unable to start QEMU as socket wasn't created in time")
				}
				v.sock, err = net.Dial("unix", filepath.ToSlash(filepath.Join(v.folder, "monitor.sock")))
				if err == nil {
					polling = false
					v.logger.Infof("Connected to socket at '%s'", filepath.ToSlash(filepath.Join(v.folder, "monitor.sock")))
				} else {
					v.logger.Infof("Attempting to dial socket at '%s'", filepath.ToSlash(filepath.Join(v.folder, "monitor.sock")))
				}
				count++
				time.Sleep(time.Second * 1)
			}
			v.state = virtualizers.Alive

			_, err = v.command.Process.Wait()
			if err == nil || err.Error() != fmt.Errorf("wait: no child processes").Error() {
				if err != nil {
					v.logger.Errorf("Error Wait Command: %s", err.Error())
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
	diskformat := "qcow2" // "raw"

	argsCommand := createArgs(o.config.VM.CPUs, o.config.VM.RAM.Units(vcfg.MiB), o.headless, diskpath, diskformat)
	argsCommand += fmt.Sprintf(" -monitor unix:%s,server,nowait", filepath.ToSlash(filepath.Join(o.folder, "monitor.sock")))

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

	if args.Start {
		err = o.Start()
		if err != nil {
			returnErr = err
		}
	}

	_, loaded := virtualizers.ActiveVMs.LoadOrStore(o.name, o.Virtualizer)
	if loaded {
		returnErr = errors.New("virtual machine already exists")
	}
}

// GenerateScript generates a .sh file to be able to run the disk with qemu standalone
func (v *Virtualizer) GenerateScript(source string) error {
	var err error

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
	//replace diskpath
	args = strings.ReplaceAll(args, filepath.ToSlash(filepath.Join(v.folder, fmt.Sprintf( /*"%s.raw"*/ "%s.qcow2", v.name))), fmt.Sprintf("\"%s\"", filepath.ToSlash(filepath.Join(source, name, fmt.Sprintf( /*"%s.raw"*/ "%s.qcow2", v.name)))))
	// replace monitor with nothing
	args = strings.ReplaceAll(args, fmt.Sprintf("-monitor unix:%s/monitor.sock,server,nowait", v.folder), "")

	f, err := os.Create(filepath.Join(source, name, "start.sh"))
	if err != nil {
		return err
	}

	f.Write([]byte(args))

	// chmod start.sh
	err = os.Chmod(f.Name(), 0777)
	if err != nil {
		return err
	}
	defer f.Close()
	return nil
}
