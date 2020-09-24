// +build windows darwin

package firecracker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/virtualizers"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
)

// DownloadPath is the path where we pull firecracker-vmlinux's from
const DownloadPath = "https://storage.googleapis.com/vorteil-dl/firecracker-vmlinux/"

func FetchBridgeDev() error {
	return errors.New("bridge devices for firecracker only supported on linux")
}
func SetupBridgeAndDHCPServer(log elog.View) error {
	return errors.New("firecracker init not supported on this operating system")
}

// Virtualizer is a struct which will implement the interface so the manager can control it
type Virtualizer struct {
	serialLogger *logger.Logger
}

// Type returns the type of virtualizer
func (v *Virtualizer) Type() string {
	return VirtualizerID
}

// Details returns data to for the ConverToVM function on util
func (v *Virtualizer) Details() (string, string, string, []virtualizers.NetworkInterface, time.Time, *vcfg.VCFG, interface{}) {
	return "", "", "", nil, time.Now(), nil, nil
}

// Initialize passes the arguments from creation to create a virtualizer. No arguments apart from name so no need to do anything
func (v *Virtualizer) Initialize(data []byte) error {
	return nil
}

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

// log writes a log line to the logger and adds prefix and suffix depending on what type of log was sent.
func (v *Virtualizer) log(logType string, text string, args ...interface{}) {

}

// log writes a log to the channel for the job
func (o *operation) log(text string, v ...interface{}) {
	o.Logs <- fmt.Sprintf(text, v...)
}

// finished finishes the job and cleans up the channels
func (o *operation) finished(err error) {

}

// updateStatus updates the status of the job to provide more feedback to the user
func (o *operation) updateStatus(text string) {
	o.Status <- text
	o.Logs <- text
}

// Logs returns virtualizer logs. Shows what to execute
func (v *Virtualizer) Logs() *logger.Logger {
	return nil
}

// Serial returns the serial logger which contains the serial output of the application
func (v *Virtualizer) Serial() *logger.Logger {
	return nil
}

// Stop stops the vm and changes it back to ready
func (v *Virtualizer) Stop() error {
	return nil
}

// State returns the state of the virtual machine
func (v *Virtualizer) State() string {
	return ""
}

// Download returns the disk
func (v *Virtualizer) Download() (vio.File, error) {
	return nil, nil
}

// Close shuts down the virtual machine and cleans up the disk and folders
func (v *Virtualizer) Close(force bool) error {
	return nil
}

// ConvertToVM is a wrapper function that provides us abilities to use the old APIs
func (v *Virtualizer) ConvertToVM() interface{} {
	return nil
}

// Prepare prepares the virtualizer with the appropriate fields to run the virtualizer
func (v *Virtualizer) Prepare(args *virtualizers.PrepareArgs) *virtualizers.VirtualizeOperation {
	return nil
}

// prepare sets the fields and arguments to spawn the virtual machine
func (o *operation) prepare(args *virtualizers.PrepareArgs) {

}

// Start create the virtualmachine and runs it
func (v *Virtualizer) Start() error {
	return nil
}
