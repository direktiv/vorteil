package virtualizers

import (
	"context"
	"encoding/base64"
	"regexp"
	"time"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vio"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
	"golang.org/x/sync/syncmap"
)

// VirtualizerAllocator is an interface which each virtualizer allocator needs to implement
type VirtualizerAllocator interface {
	Alloc() Virtualizer             // Return the virtualizer
	ValidateArgs(data []byte) error // Validates the data provided to the virtualizer
	DiskAlignment() vcfg.Bytes      // Returns the appropriate disk alignment required for the virtualizer
	DiskFormat() vdisk.Format       // Returns the appropriate disk format required for the virtualizer
	IsAvailable() bool              // Check if the virtualizer is available on the host's system
}

// Virtualizer is an interface which each virtualzier needs to implement
type Virtualizer interface {
	Type() string                                                                              // Returns what type of virtualizer it is as each virtualizer is actually a VM. I still need the type
	Initialize(data []byte) error                                                              // Initialize the virtualizer using the data provided from the alloc
	Prepare(args *PrepareArgs) *VirtualizeOperation                                            // Prepare the vm setup args etc
	State() string                                                                             // Return the state the vm is currently in
	Download() (vio.File, error)                                                               // Download the disk of the vm
	Details() (string, string, string, []NetworkInterface, time.Time, *vcfg.VCFG, interface{}) // fetch details relating to the machine
	// Detach(string) error                            // removes the vm from the active vms section and moves vm contents to different location
	Start() error           // Start the vm
	Stop() error            // Stop the vm
	Serial() *logger.Logger // Return the serial output of the vm
	Close(bool) error       // Close the vm is deleting the vm and removing its contents as its not needed anymore.
}

// Create two maps one for the virtualizers that get registered the other to track the vms that are currently created
var registeredVirtualizers map[string]VirtualizerAllocator

// ActiveVMs is a global sync map that contains current running vms
var ActiveVMs *syncmap.Map

// IPRegex is the regexp to capture an ip address from ip output of a vm
var IPRegex = regexp.MustCompile(`(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)(\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)){3}$`)

func init() {
	registeredVirtualizers = make(map[string]VirtualizerAllocator)
	ActiveVMs = new(syncmap.Map)
}

// Register Adds a allocator to the map depending on what virtualizer was created
func Register(vtype string, allocator VirtualizerAllocator) {
	registeredVirtualizers[vtype] = allocator
}

// PrepareArgs is a struct that contains what the VM needs to be able to act accordingly.
type PrepareArgs struct {
	Name      string // name of the vm
	PName     string // name of virtualizer spawned from
	Logger    elog.View
	FCPath    string // used for firecracker to find vmlinux binaries
	Context   context.Context
	Start     bool       // to control whether its to start automatically
	Config    *vcfg.VCFG // the vcfg attached to the VM
	Source    interface{}
	ImagePath string
	VMDrive   string // path to store disks for vms
}

// VirtualizeOperation is a struct that contains ways to log for the operation
// As it is normally go routined which can't return the error through normal means.
type VirtualizeOperation struct {
	Logs   <-chan string
	Status <-chan string
	Error  <-chan error
}

// NetworkProtocol is related to the API returning state
type NetworkProtocol string

// Routes contains comprehensive information about the network interfaces on a
// live virtual machine, and how to connect to them. A common example of their
// use would be as follows.
//
//	addr := Routes.NIC[0].Protocol[NetworkProtocolHTTP].Port["80"].Address
//
type Routes struct {
	NIC [4]NIC
}

// NIC contains information pertaining to a specific network card for the
// virtual machine.
type NIC struct {
	Protocol map[NetworkProtocol]*NetworkProtocolPorts
}

// NetworkProtocolPorts contains mappings from the internal ports in use by a
// Vorteil VM to the network address that can be used to reach them externally.
type NetworkProtocolPorts struct {
	Port map[string]*NetworkRoute
}

// NetworkRoute contains information representing the valid and complete
// information needed to connected to a NetworkPort, regardless of NAT or
// proxying. Standard ports may be omitted, such as port 80 on HTTP, or 443 on
// HTTPS.
//
// Examples:
// 	NetworkRoute.Address = "192.168.1.1:80"
// 	NetworkRoute.Address = "localhost:8080"
// 	NetworkRoute.Address = "http://myapp.vorteil.io"
//
type NetworkRoute struct {
	Address string
}

type Source struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Checksum   string   `json:"checksum"`
	Icon       string   `json:"icon"`
	FileSystem []string `json:"filesystem"`
	Job        string   `json:"job"`
}

type RouteMap struct {
	Port    string `json:"port"`
	Address string `json:"address"`
}

type NetworkInterface struct {
	Name    string     `json:"name"`
	IP      string     `json:"ip"`
	Mask    string     `json:"mask"`
	Gateway string     `json:"gateway"`
	UDP     []RouteMap `json:"udp"`
	TCP     []RouteMap `json:"tcp"`
	HTTP    []RouteMap `json:"http"`
	HTTPS   []RouteMap `json:"https"`
}

// Programs ..
type ProgramSummaries struct {
	Binary string   `json:"binary,omitempty"`
	Args   string   `json:"args,omitempty"`
	Env    []string `json:"env,omitempty"`
}

type VirtualMachine struct {
	ID       string    `json:"id"`
	Instance string    `json:"instance"`
	LogFile  string    `json:"logFile"`
	StateLog string    `json:"stateLog"`
	Created  time.Time `json:"created"`
	Status   string    `json:"status"`
	State    string    `json:"state"`
	Platform string    `json:"platform"`
	Source   Source    `json:"source"`

	// build-determined fields
	Kernel string     `json:"kernel"`
	CPUs   int        `json:"cpus"`
	RAM    vcfg.Bytes `json:"ram"`
	Disk   vcfg.Bytes `json:"disk"`

	Programs []ProgramSummaries `json:"programs"`

	Name    string    `json:"name"`
	Author  string    `json:"author"`
	Summary string    `json:"summary"`
	URL     string    `json:"url"`
	Version string    `json:"version"`
	Date    time.Time `json:"date"`

	// dynamic or semi-dynamic fields
	Hostname string             `json:"hostname"`
	Networks []NetworkInterface `json:"networks"`

	// Logs
	Serial LogChunk `json:"serial"`
}

type LogChunk struct {
	Cursor string `json:"cursor"`
	More   bool   `json:"more"`
	Data   string `json:"data"`
}

func (l *LogChunk) Bytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(l.Data)
}
