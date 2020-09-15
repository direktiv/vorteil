// +build linux darwin

package qemu

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-shellwords"
	"github.com/thanhpk/randstr"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/virtualizers"
)

// Start creates the virtualmachine and runs it
func (v *Virtualizer) Start() error {
	v.log("debug", "Starting VM")
	v.command = exec.Command(v.command.Args[0], v.command.Args[1:]...)
	var socketFound bool
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
				v.log("error", "Error Command Start: %v", err)
			}

			polling := true
			count := 0
			for {
				if !polling {
					break
				}
				if count == 10 {
					v.log("error", "Error: unable to start QEMU as socket wasn't created in time")
					socketFound = false
					break
				}
				v.sock, err = net.Dial("unix", filepath.ToSlash(filepath.Join(v.folder, "monitor.sock")))
				if err == nil {
					socketFound = true
					polling = false
					v.log("info", "Connected to socket at %s", filepath.ToSlash(filepath.Join(v.folder, "monitor.sock")))
				} else {
					v.log("info", "Attempting to dial socket at %s", filepath.ToSlash(filepath.Join(v.folder, "monitor.sock")))
				}
				count++
				time.Sleep(time.Second * 1)
			}
			v.state = virtualizers.Alive

			_, err = v.command.Process.Wait()
			if err == nil || err.Error() != fmt.Errorf("wait: no child processes").Error() {
				if err != nil {
					v.log("error", "Error Command Wait: %v", err)
				}
				if v.state == virtualizers.Alive {
					if !socketFound {
						v.state = virtualizers.Broken

					} else {
						err = v.Stop()
						if err != nil {
							v.log("error", "Error stopping vm: %v", err)
						}
					}

				}
			}
		}()
	}
	return nil
}

// prepare sets the fields and arguments to spawn the virtual machine
func (o *operation) prepare(args *virtualizers.PrepareArgs) {
	var returnErr error

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
	o.id = randstr.Hex(5)
	o.folder = filepath.Join(o.vmdrive, fmt.Sprintf("%s-%s", o.id, o.Type()))

	o.updateStatus(fmt.Sprintf("Copying disk to managed location"))

	err = os.MkdirAll(o.folder, os.ModePerm)
	if err != nil {
		returnErr = err
		return
	}

	diskpath := filepath.ToSlash(args.ImagePath)
	diskformat := "raw"

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

	o.Virtualizer.log("info", "Creating QEMU VM with Args: %s", strings.Join(o.command.Args, " "))

	o.log(fmt.Sprintf("Creating Qemu Virtualizer with args: %s\n", strings.Join(o.command.Args, " ")))

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
	args = strings.ReplaceAll(args, filepath.ToSlash(filepath.Join(v.folder, fmt.Sprintf("%s.raw", v.name))), fmt.Sprintf("\"%s\"", filepath.ToSlash(filepath.Join(source, name, fmt.Sprintf("%s.raw", v.name)))))
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
