// +build linux

package firecracker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	"github.com/firecracker-microvm/firecracker-go-sdk/client/models"
	log "github.com/sirupsen/logrus"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/virtualizers"
)

// operation is the job progress that gets tracked via APIs
type operation struct {
	finishedLock sync.Mutex
	isFinished   bool
	*Virtualizer
	Logs   chan string
	Status chan string
	Error  chan error
	ctx    context.Context
}

// log writes a log to the channel for the job
func (o *operation) log(text string, v ...interface{}) {
	o.Logs <- fmt.Sprintf(text, v...)
}

// finished finishes the job and cleans up the channels
func (o *operation) finished(err error) {
	o.finishedLock.Lock()
	defer o.finishedLock.Unlock()
	if o.isFinished {
		return
	}

	o.isFinished = true
	if err != nil {
		o.Logs <- fmt.Sprintf("Error: %v", err)
		o.Status <- fmt.Sprintf("Failed: %v", err)
		o.Error <- err
	}
	if err != nil {
		o.logger.Errorf("Error: %v", err)
	}

	close(o.Logs)
	close(o.Status)
	close(o.Error)
}

// updateStatus updates the status of the job to provide more feedback to the user
func (o *operation) updateStatus(text string) {
	o.Status <- text
	o.Logs <- text
}

func (o *operation) CreateFileVMLinux(kernel string) (*os.File, error) {
	// Create file locally to download
	file, err := os.Create(filepath.Join(o.firecrackerPath, kernel))
	if err != nil {
		return file, err
	}
	return file, err
}

func (o *operation) InitializeDownloadVMLinux(kernel string, f *os.File) (int, error) {
	// file doesn't exist must download from bucket
	o.updateStatus(fmt.Sprintf("VMLinux for kernel doesn't exist downloading..."))
	// Download vmlinux from google
	url := DownloadPath + kernel
	client := http.DefaultClient

	// Determinate the file size
	resp, err := client.Head(url)
	if err != nil {
		os.Remove(f.Name())
		return 0, err
	}
	if resp.StatusCode == 404 {
		os.Remove(f.Name())
		return 0, fmt.Errorf("Kernel '%s' VMLinux does not exist", kernel)
	}
	defer resp.Body.Close()
	contentLength := resp.Header.Get("content-length")
	length, err := strconv.Atoi(contentLength)
	if err != nil {
		os.Remove(f.Name())
		return 0, err
	}
	return length, nil
}

// fetchVMLinux reads the kernel it wants to run and returns the vm linux required to run
// Will download the vmlinux if it doesn't exist
func (o *operation) fetchVMLinux(kernel string) (string, error) {
	client := http.DefaultClient

	progress := o.logger.NewProgress("Fetching VMLinux", "", 0)
	defer progress.Finish(false)
	o.updateStatus(fmt.Sprintf("Fetching VMLinux searching %s for %s", o.firecrackerPath, kernel))
	// check if vmlinux is on system at valid path
	_, err := os.Stat(filepath.Join(o.firecrackerPath, kernel))
	if err != nil {
		f, err := o.CreateFileVMLinux(kernel)
		if err != nil {
			return "", err
		}
		defer f.Close()
		length, err := o.InitializeDownloadVMLinux(kernel, f)
		if err != nil {
			return "", err
		}
		// Make request
		resp, err := client.Get(DownloadPath + kernel)
		if err != nil {
			os.Remove(f.Name())
			return "", err
		}
		defer resp.Body.Close()
		// pipe stream
		body := io.TeeReader(resp.Body, newWriter(int64(length), func(downloaded, total int64) {
			o.updateStatus(fmt.Sprintf("Downloading VMLinux(%s/%s)", ByteCountDecimal(downloaded), ByteCountDecimal(total)))
		}))
		_, err = io.Copy(f, body)
		if err != nil {
			os.Remove(f.Name())
			return "", err
		}
	}

	return filepath.Join(o.firecrackerPath, kernel), nil
}

// makeInterfaces creates the firecracker network interfaces and returns
func (o *operation) makeInterfaces() []firecracker.NetworkInterface {
	var interfaces []firecracker.NetworkInterface
	for i := 0; i < len(o.tapDevice.Devices); i++ {
		interfaces = append(interfaces,
			firecracker.NetworkInterface{
				StaticConfiguration: &firecracker.StaticNetworkConfiguration{
					HostDevName: o.tapDevice.Devices[i],
				},
			},
		)
	}
	return interfaces
}

func (o *operation) generateFirecrackerConfig(diskpath string) (*firecracker.Config, []firecracker.Opt, error) {
	var err error
	logger := log.New()
	logger.SetFormatter(&log.TextFormatter{
		DisableColors: false,
		ForceColors:   true,
	})
	logger.Out = o

	devices := []models.Drive{}
	rootDrive := models.Drive{
		DriveID:      firecracker.String("1"),
		PathOnHost:   &diskpath,
		IsRootDevice: firecracker.Bool(true),
		IsReadOnly:   firecracker.Bool(false),
		Partuuid:     vimg.Part2UUIDString,
	}

	devices = append(devices, rootDrive)

	o.kip, err = o.fetchVMLinux(o.config.VM.Kernel)
	if err != nil {
		return nil, nil, err
	}

	o.tapDevice, err = CreateTapDevices(o.id, len(o.routes))
	if err != nil {
		return nil, nil, err
	}
	interfaces := o.makeInterfaces()

	fcfg := firecracker.Config{
		SocketPath:      filepath.Join(o.folder, fmt.Sprintf("%s.%s", o.name, "socket")),
		KernelImagePath: o.kip,
		KernelArgs:      fmt.Sprintf("init=/vorteil/vinitd root=PARTUUID=%s reboot=k panic=1 pci=off vt.color=0x00", vimg.Part2UUIDString),
		Drives:          devices,
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(int64(o.config.VM.CPUs)),
			HtEnabled:  firecracker.Bool(false),
			MemSizeMib: firecracker.Int64(int64(o.config.VM.RAM.Units(vcfg.MiB))),
		},
		NetworkInterfaces: interfaces,
		ForwardSignals:    []os.Signal{},
	}

	machineOpts := []firecracker.Opt{
		firecracker.WithLogger(log.NewEntry(logger)),
	}
	return &fcfg, machineOpts, nil
}

// prepare sets the fields and arguments to spawn the virtual machine
func (o *operation) prepare(args *virtualizers.PrepareArgs) {
	var returnErr error

	o.updateStatus(fmt.Sprintf("Building firecracker command and tap interfaces..."))
	defer func() {
		o.finished(returnErr)
	}()

	o.state = "initializing"
	o.name = args.Name
	err := os.MkdirAll(args.FCPath, os.ModePerm)
	if err != nil {
		returnErr = err
		return
	}
	o.folder = filepath.Dir(args.ImagePath)
	o.id = strings.Split(filepath.Base(o.folder), "-")[1]
	ctx := context.Background()
	vmmCtx, vmmCancel := context.WithCancel(ctx)
	diskpath := filepath.ToSlash(args.ImagePath)

	// append new fields to overarching struct
	o.fconfig, o.machineOpts, err = o.generateFirecrackerConfig(diskpath)
	if err != nil {
		returnErr = err
		return
	}

	o.gctx = ctx
	o.vmmCtx = vmmCtx
	o.vmmCancel = vmmCancel

	o.state = "ready"

	_, loaded := virtualizers.ActiveVMs.LoadOrStore(o.name, o.Virtualizer)
	if loaded {
		returnErr = errors.New("virtual machine already exists")
		return
	}

	if args.Start {
		err = o.Start()
		if err != nil {
			returnErr = err
			return
		}
	}
}
