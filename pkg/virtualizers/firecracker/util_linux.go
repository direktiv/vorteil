// +build linux

package firecracker

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	dhcp "github.com/krolaw/dhcp4"
	conn "github.com/krolaw/dhcp4/conn"
	"github.com/milosgajdos/tenus"
	"github.com/songgao/water"
	"github.com/vorteil/vorteil/pkg/elog"
	dhcpHandler "github.com/vorteil/vorteil/pkg/virtualizers/dhcp"
)

// FetchBridgeDev attempts to retrieve the bridge device
func FetchBridgeDev() error {
	// Check if bridge device exists
	_, err := tenus.BridgeFromName(vorteilBridge)
	if err != nil {
		return errors.New("try running 'vorteil firecracker-setup' before using firecracker")
	}
	return err
}

// SetupBridgeAndDHCPServer creates the bridge which provides DHCP addresses todo
// firecracker instances.
func SetupBridgeAndDHCPServer(log elog.View) error {

	log.Printf("creating bridge %s", vorteilBridge)
	// Create bridge device
	bridger, err := tenus.NewBridgeWithName(vorteilBridge)
	if err != nil {
		if !strings.Contains(err.Error(), "Interface name vorteil-bridge already assigned on the host") {
			return err
		}
		// get bridge device
		bridger, err = tenus.BridgeFromName(vorteilBridge)
		if err != nil {
			return err
		}
	}
	// Switch bridge up
	if err = bridger.SetLinkUp(); err != nil {
		return err
	}
	// Fetch address
	ipv4Addr, ipv4Net, err := net.ParseCIDR("174.72.0.1/24")
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

	log.Printf("starting dhcp server")

	// create dhcp server on an interface
	server := dhcpHandler.NewHandler()
	pc, err := conn.NewUDP4BoundListener(vorteilBridge, ":67")
	if err != nil {
		return err
	}

	// create server handler to create tap devices under sudo
	http.HandleFunc("/", OrganiseTapDevices)
	go func() {
		http.ListenAndServe(":7476", nil)
	}()
	log.Printf("Listening on '7476' for creating and deleting TAP devices")
	log.Printf("Listening on 'vorteil-bridge' for DHCP requests")
	// Start dhcp server to listen
	dhcp.Serve(pc, server)

	return nil
}

// CreateDevices is a struct used to tell the process to create TAP devices via a rest request
type CreateDevices struct {
	Id     string `json:"id"`
	Routes int    `json:"count"`
}

// Devices is a struct used to tell the process to deleted TAP devices via a delete request
type Devices struct {
	Devices []string `json:"devices"`
}

func createTaps(w http.ResponseWriter, cd CreateDevices, t tenus.Bridger) []string {
	var tapDevices []string

	for i := 0; i < cd.Routes; i++ {
		ifceName := fmt.Sprintf("%s-%s", cd.Id, strconv.Itoa(i))

		// create tap device
		config := water.Config{
			DeviceType: water.TAP,
		}
		config.Name = ifceName
		config.Persist = true
		ifce, err := water.New(config)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		// close interface so firecracker can read it
		err = ifce.Close()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}

		// get tap device
		linkDev, err := tenus.NewLinkFrom(ifceName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		//set tap device up
		err = linkDev.SetLinkUp()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		// add network interface to bridge
		err = t.AddSlaveIfc(linkDev.NetInterface())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		tapDevices = append(tapDevices, ifceName)
	}

	return tapDevices
}

func createDevices(w http.ResponseWriter, r *http.Request) {
	var cd CreateDevices

	err := json.NewDecoder(r.Body).Decode(&cd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}

	// get bridge device
	bridgeDev, err := tenus.BridgeFromName(vorteilBridge)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}

	// set network adapters
	if cd.Routes > 0 {
		// write interfaces back
		returnDevices := &Devices{
			Devices: createTaps(w, cd, bridgeDev),
		}
		body, err := json.Marshal(returnDevices)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

		}
		io.Copy(w, bytes.NewBuffer(body))
	}
}

// OrganiseTapDevices handles http requests to create and delete tap interfaces for firecracker
func OrganiseTapDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		createDevices(w, r)
	case http.MethodDelete:
		deleteDevices(w, r)
	default:
		http.Error(w, "method not available", http.StatusBadRequest)
	}
}

func deleteDevices(w http.ResponseWriter, r *http.Request) {
	var dd Devices
	err := json.NewDecoder(r.Body).Decode(&dd)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
	}

	for i := 0; i < len(dd.Devices); i++ {
		err := tenus.DeleteLink(dd.Devices[i])
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	}
	w.WriteHeader(http.StatusOK)
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
		return 0, fmt.Errorf("Kernel '%s' VMLinux does not exist", kernel)
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
	defer file.Close()

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
		o.updateStatus(fmt.Sprintf("VMLinux for kernel doesn't exist downloading..."))
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
