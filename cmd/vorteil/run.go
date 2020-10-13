package main

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	isatty "github.com/mattn/go-isatty"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/thanhpk/randstr"
	"github.com/vorteil/vorteil/pkg/imagetools"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	"github.com/vorteil/vorteil/pkg/virtualizers/util"
	"github.com/vorteil/vorteil/pkg/vpkg"
)

var runCmd = &cobra.Command{
	Use:   "run [RUNNABLE]",
	Short: "Quick-launch a virtual machine",
	Long: `The run command is a convenience function for quickly getting a Vorteil machine
up and running. It attempts to emulate the behaviour of running the binary
natively as best as possible, which includes making it superficially appear as
though the virtual machine is a child process of the CLI by handling interrupts
and cleaning up the instance when it's done.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		buildablePath := "."
		if len(args) >= 1 {
			buildablePath = args[0]
		}

		pkgBuilder, err := getPackageBuilder("BUILDABLE", buildablePath)
		if err != nil {
			SetError(err, 2)
			return
		}
		defer pkgBuilder.Close()

		err = modifyPackageBuilder(pkgBuilder)
		if err != nil {
			SetError(err, 3)
			return
		}

		pkgReader, err := vpkg.ReaderFromBuilder(pkgBuilder)
		if err != nil {
			SetError(err, 4)
			return
		}
		defer pkgReader.Close()

		pkgReader, err = vpkg.PeekVCFG(pkgReader)
		if err != nil {
			SetError(err, 5)
			return
		}

		cfgf := pkgReader.VCFG()
		cfg, err := vcfg.LoadFile(cfgf)
		if err != nil {
			SetError(err, 6)
			return
		}
		err = initKernels()
		if err != nil {
			SetError(err, 7)
			return
		}

		src, _, err := readSourcePath(buildablePath)
		if err != nil {
			SetError(err, 20)
			return
		}

		// Fetch name of the app from path
		var name string
		_, err = os.Stat(src)
		if err != nil {
			// If stat errors assume its a url
			u, errParse := url.Parse(buildablePath)
			if errParse == nil {
				// Check if its a url i can handle otherwise default to vorteil-vm
				if u.Hostname() == "apps.vorteil.io" {
					name = u.Path
					name = strings.ReplaceAll(name, "/file/", "")
					name = strings.ReplaceAll(name, "/", "-")
				} else {
					name = "vorteil-vm"
				}
			} else {
				SetError(err, 1)
				return
			}
		} else {
			name = strings.ReplaceAll(filepath.Base(buildablePath), ".vorteil", "")
		}

		switch flagPlatform {
		case platformQEMU:
			err = runQEMU(pkgReader, cfg, name)
			if err != nil {
				SetError(err, 8)
				return
			}
		case platformVirtualBox:
			err = runVirtualBox(pkgReader, cfg, name)
			if err != nil {
				SetError(err, 9)
				return
			}
		case platformHyperV:
			err = runHyperV(pkgReader, cfg, name)
			if err != nil {
				SetError(err, 10)
				return
			}
		case platformFirecracker:
			err = runFirecracker(pkgReader, cfg, name)
			if err != nil {
				SetError(err, 11)
				return
			}
		default:
			if flagPlatform == "not installed" {
				SetError((fmt.Errorf("no virtualizers are currently installed")), 12)
			} else {
				SetError((fmt.Errorf("platform '%s' not supported", flagPlatform)), 12)
			}
		}

	},
}

func init() {
	f := runCmd.Flags()
	f.StringVar(&flagPlatform, "platform", defaultVirtualizer(), "run a virtual machine with appropriate hypervisor (qemu, firecracker, virtualbox, hyper-v)")
	f.BoolVar(&flagGUI, "gui", false, "when running virtual machine show gui of hypervisor")
	f.BoolVar(&flagShell, "shell", false, "add a busybox shell environment to the image")
	f.StringVar(&flagRecord, "record", "", "")
}

func defaultVirtualizer() string {
	defaultP := "not installed"
	backends, _ := virtualizers.Backends()
	for _, installed := range backends {
		if installed == "vmware" {
			continue
		}
		if installed == "qemu" {
			defaultP = "qemu"
		} else if installed == "hyperv" {
			defaultP = "hyperv"
		} else if installed == "virtualbox" {
			defaultP = "virtualbox"
		} else if installed == "firecracker" {
			defaultP = "firecracker"
		}
		break
	}
	return defaultP
}

func runDecompile(diskpath string, outpath string, skipUnTouched bool) error {
	iio, err := vdecompiler.Open(diskpath)
	if err != nil {
		return err
	}

	defer iio.Close()

	report, err := imagetools.DecompileImage(iio, outpath, skipUnTouched)
	if err != nil {
		return err
	}

	for _, dFile := range report.ImageFiles {
		switch dFile.Result {
		case imagetools.CopiedMkDir:
			log.Debugf("Creating Dir > %s", dFile.Path)
		case imagetools.CopiedRegularFile:
			log.Debugf("Copied File  > %s", dFile.Path)
		case imagetools.CopiedSymlink:
			log.Debugf("Created Symlink > %s", dFile.Path)
		case imagetools.SkippedAbnormalFile:
			log.Debugf("Skipped Abnormal > %s", dFile.Path)
		case imagetools.SkippedNotTouched:
			log.Debugf("Skipped Untouched File > %s", dFile.Path)
		}
	}

	return nil
}
func run(virt virtualizers.Virtualizer, diskpath string, cfg *vcfg.VCFG, name string) error {

	// Gather home directory for firecracker storage path
	home, err := homedir.Dir()
	if err != nil {
		return err
	}

	vo := virt.Prepare(&virtualizers.PrepareArgs{
		Name:      fmt.Sprintf("%s-%s", name, randstr.Hex(4)),
		PName:     virt.Type(),
		Start:     true,
		Config:    cfg,
		FCPath:    filepath.Join(home, ".vorteild", "firecracker-vm"),
		ImagePath: diskpath,
		Logger:    log,
	})

	serial := virt.Serial()
	serialSubscription := serial.Subscribe()
	s := serialSubscription.Inbox()
	defer serialSubscription.Close()
	defer serial.Close()

	signalChannel, chBool := listenForInterrupt()

	var finished bool
	var routesChecked bool

	defer func() {
		virt.Close(true)

		if flagRecord != "" {
			decompileSpinner := log.NewProgress("Decompiling Disk", "", 0)
			defer decompileSpinner.Finish(true)
			if err := runDecompile(diskpath, flagRecord, flagTouched); err != nil {
				SetError(err, 1)
				return
			}
			decompileSpinner.Finish(true)
			log.Printf("Decompile Completed")
		}
	}()

	var prepareError error
	// listen to prepare operation
	go func() {
		select {
		case err, errch := <-vo.Error:
			if !errch {
				break
			}
			if err != nil {
				prepareError = err
			}
		}
	}()

	var hasBeenAlive bool
	for {
		select {
		case <-time.After(time.Millisecond * 200):
			// Check prepare error from vm operation
			if prepareError != nil {
				return prepareError
			}
			if virt.State() == virtualizers.Alive && !routesChecked {
				routesChecked = true
				lines := gatherNetworkDetails(util.ConvertToVM(virt.Details()).(*virtualizers.VirtualMachine))
				if len(lines) > 0 {
					log.Warnf("Network settings")
					for _, line := range lines {
						log.Warnf(line)
					}
				}
			}
			// Check when vm has become alive
			if virt.State() == virtualizers.Alive && !hasBeenAlive {
				hasBeenAlive = true
			}
			// vm has been stopped and has been alive before
			if virt.State() == virtualizers.Ready && hasBeenAlive {
				return nil
			}
		case msg, more := <-s:
			if !more {
				return nil
			}
			fmt.Print(string(msg))
		case <-signalChannel:
			if finished {
				return nil
			}
			// Close virtual machine without forcing to handle stopping the virtual machine gracefully
			go func() {
				err = virt.Stop()
				if err != nil {
					log.Errorf(err.Error())
				}
			}()
			finished = true

		case <-chBool:
			return nil
		}
	}

}

func fetchPorts(lines []string, portmap virtualizers.RouteMap, networkType string) []string {
	actual := portmap.Address[strings.LastIndex(portmap.Address, ":")+1:]
	if actual != portmap.Port && actual != "" {
		port2 := portmap.Address
		if port2 == "" {
			port2 = portmap.Port
		}
		lines = append(lines, fmt.Sprintf(" • %s:%s → %s", networkType, portmap.Port, port2))
	} else {
		lines = append(lines, fmt.Sprintf(" • %s:%s", networkType, portmap.Port))
	}
	return lines
}

// Fetch network details about virtual machine
func gatherNetworkDetails(machine *virtualizers.VirtualMachine) []string {
	var lines []string
	for _, network := range machine.Networks {
		for _, portmap := range network.UDP {
			var udp []string
			udp = append(udp, fetchPorts(udp, portmap, "udp")...)
			lines = append(lines, udp...)
		}
		for _, portmap := range network.TCP {
			var tcp []string
			tcp = append(tcp, fetchPorts(tcp, portmap, "tcp")...)
			lines = append(lines, tcp...)
		}
		for _, portmap := range network.HTTP {
			var http []string
			http = append(http, fetchPorts(http, portmap, "http")...)
			lines = append(lines, http...)
		}
		for _, portmap := range network.HTTPS {
			var https []string
			https = append(https, fetchPorts(https, portmap, "https")...)
			lines = append(lines, https...)
		}
	}
	return lines
}

func raw(start bool) error {
	r := "raw"
	if !start {
		r = "-raw"
	}

	rawMode := exec.Command("stty", r)
	rawMode.Stdin = os.Stdin
	err := rawMode.Run()
	if err != nil {
		return err
	}

	return nil
}

func listenForInterrupt() (chan os.Signal, chan bool) {
	var signalChannel chan os.Signal
	signalChannel = make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)
	chBool := make(chan bool, 1)

	// check if this is running in a sygwin terminal, interrupt signals are difficult to capture
	if isatty.IsCygwinTerminal(os.Stdout.Fd()) {

		go func() {
			raw(true)
			for {
				inp := bufio.NewReader(os.Stdin)
				r, _, _ := inp.ReadRune()

				if r == '\x03' { // ctrl+c
					chBool <- true
					break
				}
			}
			raw(false)
		}()
	}
	return signalChannel, chBool
}
