// +build linux

package firecracker

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/milosgajdos/tenus"
	"github.com/vishvananda/netlink"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/virtualizers/iputil"
)

const (
	tunPath = "/dev/net/tun"

	cIFFTAP  = 0x0002
	cIFFNOPI = 0x1000
)

type ifReq struct {
	Name  [0x10]byte
	Flags uint16
	pad   [0x28 - 0x10 - 2]byte
}

func ioctl(fd uintptr, request uintptr, argp uintptr) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(request), argp)
	if errno != 0 {
		return os.NewSyscallError("ioctl", errno)
	}
	return nil
}

// createIfc creates an interface and returns an error if unsuccessful
func createIfc(name string) error {

	var (
		tunfd int
		err   error
	)
	// delete the interface if something failed
	defer func() {
		if err != nil {
			tenus.DeleteLink(name)
		}
	}()

	if tunfd, err = syscall.Open(tunPath, os.O_RDWR|syscall.O_NONBLOCK, 0); err != nil {
		return err
	}
	defer syscall.Close(tunfd)

	var req ifReq
	req.Flags = cIFFTAP | cIFFNOPI
	copy(req.Name[:], name)

	err = ioctl(uintptr(tunfd), syscall.TUNSETIFF, uintptr(unsafe.Pointer(&req)))
	if err != nil {
		return err
	}

	req2 := 1
	err = ioctl(uintptr(tunfd), syscall.TUNSETPERSIST, uintptr(unsafe.Pointer(&req2)))
	if err != nil {
		return err
	}

	link, err := netlink.LinkByName(name)
	if err != nil {
		return err
	}

	err = netlink.LinkSetUp(link)
	if err != nil {
		return err
	}

	return nil

}

// FetchBridgeDevice check if the bridge exists
func FetchBridgeDevice() error {
	_, err := tenus.BridgeFromName(vorteilBridge)
	if err != nil {
		return err
	}
	return nil
}

// SetupBridgeAndDHCPServer creates the bridge which provides DHCP addresses todo
// firecracker instances.
func SetupBridge(log elog.View, ip string) error {

	bridgeExists := true
	// Create bridge device
	bridger, err := tenus.NewBridgeWithName(vorteilBridge)
	if err != nil {
		bridgeExists = false
		if !strings.Contains(err.Error(), "Interface name vorteil-bridge already assigned on the host") {
			return err
		}
		// get bridge device
		bridger, err = tenus.BridgeFromName(vorteilBridge)
		if err != nil {
			return err
		}
	}

	if !bridgeExists {
		log.Printf("creating bridge %s", vorteilBridge)
	}

	// Switch bridge up
	if err = bridger.SetLinkUp(); err != nil {
		return err
	}
	cidr := fmt.Sprintf("%s/%s", ip, iputil.BaseMask)

	// Fetch address
	ipv4Addr, ipv4Net, err := net.ParseCIDR(cidr)
	if err != nil {
		if !strings.Contains(err.Error(), "file exists") {
			return err
		}
	}
	// Assign bridge to device so host knows where to send requests.
	if err = bridger.SetLinkIp(ipv4Addr, ipv4Net); err != nil {
		if !strings.Contains(err.Error(), "file exists") {
			return err
		}
	}

	return nil
}

// writeCounter counts the number of bytes written to it.
type writeCounter struct {
	total      int64 // total size
	downloaded int64 // downloaded # of bytes transferred
	onProgress func(downloaded int64, total int64)
}

// Write implements the io.Writer interface.
//
// Always completes and never returns an error.
func (wc *writeCounter) Write(p []byte) (n int, e error) {
	n = len(p)
	wc.downloaded += int64(n)
	wc.onProgress(wc.downloaded, wc.total)
	return
}
func newWriter(size int64, onProgress func(downloaded, total int64)) io.Writer {
	return &writeCounter{total: size, onProgress: onProgress}
}

func (o *operation) fetchLength(file *os.File, client *http.Client, url string, kernel string) (int, error) {
	// Determinate the file size
	resp, err := client.Head(url)
	if err != nil {
		os.Remove(file.Name())
		return 0, err
	}
	if resp.StatusCode == 404 {
		os.Remove(file.Name())
		return 0, fmt.Errorf("'%s' VMLinux, does not exist", kernel)
	}
	length, err := strconv.Atoi(resp.Header.Get("content-length"))
	if err != nil {
		return 0, err
	}
	return length, nil
}
func (o *operation) createFileAndGetLength(kernel string, client *http.Client, url string) (*os.File, int, error) {
	// Create file locally to download
	file, err := os.Create(filepath.Join(o.firecrackerPath, kernel))
	if err != nil {
		return nil, 0, err
	}
	// defer file.Close()

	length, err := o.fetchLength(file, client, url, kernel)
	if err != nil {
		os.Remove(file.Name())
		return nil, 0, err
	}

	return file, length, nil
}

// fetchVMLinux reads the kernel it wants to run and returns the vm linux required to run
// Will download the vmlinux if it doesn't exist
func (o *operation) fetchVMLinux(kernel string) (string, error) {
	o.updateStatus(fmt.Sprintf("Fetching VMLinux searching %s for %s", o.firecrackerPath, kernel))
	// check if vmlinux is on system at valid path
	_, err := os.Stat(filepath.Join(o.firecrackerPath, kernel))
	if err != nil {
		// file doesn't exist must download from bucket
		o.updateStatus(fmt.Sprintf("'%s' does not exist, downloading...", kernel))
		// Download vmlinux from google
		url := DownloadPath + kernel
		client := http.DefaultClient

		file, length, err := o.createFileAndGetLength(kernel, client, url)
		if err != nil {
			return "", err
		}

		// Make request
		resp, err := client.Get(url)
		if err != nil {
			os.Remove(file.Name())
			return "", err
		}
		defer resp.Body.Close()
		p := o.logger.NewProgress("Downloading VMLinux", "Bytes", int64(length))
		defer p.Finish(false)
		// pipe stream
		var pDownloaded = int64(0)
		body := io.TeeReader(resp.Body, newWriter(int64(length), func(downloaded, total int64) {
			p.Increment(downloaded - pDownloaded)
			pDownloaded = downloaded
		}))
		_, err = io.Copy(file, body)
		if err != nil {
			os.Remove(file.Name())
			return "", err
		}
		defer file.Close()
	}

	return filepath.Join(o.firecrackerPath, kernel), nil
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

	close(o.Logs)
	close(o.Status)
	close(o.Error)
}

// updateStatus updates the status of the job to provide more feedback to the user
func (o *operation) updateStatus(text string) {
	o.Status <- text
	o.Logs <- text
}
