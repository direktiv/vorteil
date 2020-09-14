// +build windows

package qemu

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mattn/go-shellwords"
	"github.com/natefinch/npipe"
	"github.com/thanhpk/randstr"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/virtualizers"
)

// Start creates the virtualmachine and runs it
func (v *Virtualizer) Start() error {

	v.log("debug", "Starting VM")
	v.command = exec.Command(v.command.Args[0], v.command.Args[1:]...)
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
			conn, err := npipe.Dial(fmt.Sprintf("\\\\.\\pipe\\%s", v.id))
			if err != nil {
				v.log("error", "Error dialing pipe: %v", err)
			}
			v.sock = conn
			go io.Copy(ioutil.Discard, conn)
			v.state = virtualizers.Alive

			_, err = v.command.Process.Wait()
			if err == nil || err.Error() != fmt.Errorf("wait: no child processes").Error() {
				if err != nil {
					v.log("error", "Error Command Wait: %v", err)
				}
				if v.state == virtualizers.Alive {
					err = v.Stop()
					if err != nil {
						v.log("error", "Error stopping vm: %v", err)
					}
				}
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

	f, err := os.Create(filepath.Join(o.folder, o.name+".raw"))
	if err != nil {
		returnErr = err
		return
	}

	_, err = io.Copy(f, args.Image)
	if err != nil {
		returnErr = err
		return
	}

	err = f.Sync()
	if err != nil {
		o.Virtualizer.log("error", "Error syncing disk: %v", err)
		returnErr = err
		return
	}

	err = f.Close()
	if err != nil {
		o.Virtualizer.log("error", "Error closing disk: %v", err)
		returnErr = err
		return
	}
	o.disk = f

	diskpath := filepath.ToSlash(o.disk.Name())
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

	o.Virtualizer.log("info", "Creating QEMU VM with Args: %s", strings.Join(o.command.Args, " "))

	o.log(fmt.Sprintf("Creating Qemu Virtualizer with args: %s\n", strings.Join(o.command.Args, " ")))

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
	args = strings.ReplaceAll(args, filepath.ToSlash(filepath.Join(v.folder, fmt.Sprintf("%s.raw", v.name))), fmt.Sprintf("\"%s\"", filepath.ToSlash(filepath.Join(source, name, fmt.Sprintf("%s.raw", v.name)))))
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
