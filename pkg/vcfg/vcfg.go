package vcfg

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/imdario/mergo"
	"github.com/sisatech/toml"
	"github.com/vorteil/vorteil/pkg/vio"
)

// VCFG ..
type VCFG struct {
	Programs []Program          `toml:"program,omitempty" json:"program,omitempty"`
	Networks []NetworkInterface `toml:"network,omitempty" json:"network,omitempty"`
	System   SystemSettings     `toml:"system,omitempty" json:"system,omitempty"`
	Info     PackageInfo        `toml:"info,omitempty" json:"info,omitempty"`
	VM       VMSettings         `toml:"vm,omitempty" json:"vm,omitempty"`
	NFS      []NFSSettings      `toml:"nfs,omitempty" json:"nfs,omitempty"`
	Routing  []Route            `toml:"route,omitempty" json:"route,omitempty"`
	Logging  []Logging          `toml:"logging,omitempty" json:"logging,omitempty"`
	Sysctl   map[string]string  `toml:"sysctl,omitempty" json:"sysctl,omitempty"`
	modtime  time.Time
}

type Privilege string

var (
	RootPrivilege      = Privilege("root")
	SuperuserPrivilege = Privilege("superuser")
	UserPrivilege      = Privilege("user")
)

// Program ..
type Program struct {
	Binary    string    `toml:"binary,omitempty" json:"binary"`
	Args      string    `toml:"args,omitempty" json:"args"`
	Env       []string  `toml:"env,omitempty" json:"env"`
	Cwd       string    `toml:"cwd,omitempty" json:"cwd"`
	Stdout    string    `toml:"stdout,omitempty" json:"stdout"`
	Stderr    string    `toml:"stderr,omitempty" json:"stderr"`
	Bootstrap []string  `toml:"bootstrap,ommitempty" json:"bootstrap"`
	LogFiles  []string  `toml:"logfiles,omitempty" json:"logfiles"`
	Privilege Privilege `toml:"privilege,omitempty" json:"privilege"`
	Strace    bool      `toml:"strace,omitempty" json:"strace"`
}

// NetworkInterface ..
type NetworkInterface struct {
	IP                               string   `toml:"ip,omitempty" json:"ip"`
	Mask                             string   `toml:"mask,omitempty" json:"mask,omitempty"`
	Gateway                          string   `toml:"gateway,omitempty" json:"gateway,omitempty"`
	UDP                              []string `toml:"udp,omitempty" json:"udp,omitempty"`
	TCP                              []string `toml:"tcp,omitempty" json:"tcp,omitempty"`
	HTTP                             []string `toml:"http,omitempty" json:"http,omitempty"`
	HTTPS                            []string `toml:"https,omitempty" json:"https,omitempty"`
	MTU                              uint     `toml:"mtu,omitzero" json:"mtu,omitempty"`
	DisableTCPSegmentationOffloading bool     `toml:"disable-tso,omitempty" json:"disable-tso,omitempty"`
	TCPDUMP                          bool     `toml:"tcpdump,omitempty" json:"tcpdump"`
}

// NFSSettings ..
type NFSSettings struct {
	MountPoint string `toml:"mount,omitempty" json:"mount"`
	Server     string `toml:"server,omitempty" json:"server"`
	Arguments  string `toml:"options,omitempty" json:"options"`
}

// Route ..
type Route struct {
	Interface   string `toml:"interface,omitempty" json:"interface,omitempty"`
	Destination string `toml:"destination,omitempty" json:"destination,omitempty"`
	Gateway     string `toml:"gateway,omitempty" json:"gateway,omitempty"`
}

// SystemSettings ..
type SystemSettings struct {
	DNS        []string   `toml:"dns,omitempty" json:"dns,omitempty"`
	NTP        []string   `toml:"ntp,omitempty" json:"ntp,omitempty"`
	Hostname   string     `toml:"hostname,omitempty" json:"hostname,omitempty"`
	MaxFDs     uint       `toml:"max-fds,omitzero" json:"max-fds,omitempty"`
	StdoutMode StdoutMode `toml:"output-mode,omitzero" json:"stdout-mode,omitempty"`
	KernelArgs string     `toml:"kernel-args,omitempty" json:"kernel-args,omitempty"`
	Filesystem Filesystem `toml:"filesystem,omitempty" json:"filesystem,omitempty"`
	User       string     `toml:"user,omitempty" json:"user,omitempty"` // Note: should we validate against regex ^[a-z]*$
}

// PackageInfo ..
type PackageInfo struct {
	Name        string    `toml:"name,omitempty" json:"name,omitempty"`
	Author      string    `toml:"author,omitempty" json:"author,omitempty"`
	Summary     string    `toml:"summary,omitempty" json:"summary,omitempty"`
	Description string    `toml:"description,omitempty" json:"description,omitempty"`
	URL         URL       `toml:"url,omitempty" json:"url,omitempty"`
	Date        Timestamp `toml:"date,omitempty,omitzero" json:"date,omitempty"`
	Version     string    `toml:"version,omitempty" json:"version,omitempty"`
}

// VMSettings ..
type VMSettings struct {
	CPUs     uint        `toml:"cpus,omitzero" json:"cpus,omitempty"`
	RAM      Bytes       `toml:"ram,omitzero" json:"ram,omitempty"`
	Inodes   InodesQuota `toml:"inodes,omitzero" json:"inodes,omitempty"`
	Kernel   string      `toml:"kernel,omitempty" json:"kernel,omitempty"`
	DiskSize Bytes       `toml:"disk-size,omitzero" json:"disk-size,omitempty"`
}

// Logging ..
type Logging struct {
	Config []string `toml:"config,omitempty" json:"config,omitempty"`
	Type   string   `toml:"type,omitempty" json:"type,omitempty"`
}

// Format ..
func (vcfg *VCFG) Format(f fmt.State, c rune) {
	data, err := json.MarshalIndent(vcfg, "", "  ")
	if err != nil {
		f.Write([]byte("FAILED TO FORMAT VCFG FOR fmt!"))
		return
	}

	f.Write(data)
}

// File ..
func (vcfg *VCFG) File() (vio.File, error) {

	data, err := vcfg.Marshal()
	if err != nil {
		return nil, err
	}

	return vio.CustomFile(vio.CustomFileArgs{
		Size:       len(data),
		ModTime:    vcfg.modtime,
		Name:       "vcfg",
		ReadCloser: ioutil.NopCloser(bytes.NewReader(data)),
	}), nil
}

// Load ..
func Load(data []byte) (*VCFG, error) {
	vcfg := new(VCFG)
	err := toml.Unmarshal(data, vcfg)
	if err != nil {
		return nil, err
	}

	vcfg.negate()
	return vcfg, err
}

// LoadFile ..
func LoadFile(f vio.File) (*VCFG, error) {
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	vcfg, err := Load(data)
	if err != nil {
		return nil, err
	}
	vcfg.modtime = f.ModTime()
	return vcfg, nil
}

// LoadFilepath ..
func (vcfg *VCFG) LoadFilepath(path string) error {

	f, err := vio.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return vcfg.LoadFile(f)
}

// ModTime ..
func (vcfg *VCFG) ModTime() time.Time {
	return vcfg.modtime
}

// Load ..
func (vcfg *VCFG) Load(data []byte) error {

	old := *vcfg

	x := new(VCFG)
	_, err := toml.Decode(string(data), x)
	if err != nil {
		return err
	}

	v, err := Merge(vcfg, x)
	if err != nil {
		return err
	}
	vcfg = v

	if !reflect.DeepEqual(vcfg, old) {
		vcfg.modtime = time.Now()
	}

	return nil
}

// Merge ..
func (vcfg *VCFG) Merge(x *VCFG) error {

	oldOriginalModtime := vcfg.modtime
	oldOriginal := fmt.Sprintf("%v", vcfg)

	oldOverwriteModtime := x.modtime
	oldOverwrite := fmt.Sprintf("%v", x)

	x, err := Merge(vcfg, x)
	if err != nil {
		return err
	}
	vcfg = x

	merged := fmt.Sprintf("%v", vcfg)

	if merged == oldOverwrite {
		vcfg.modtime = oldOverwriteModtime
	} else if merged == oldOriginal {
		vcfg.modtime = oldOriginalModtime
	} else {
		vcfg.modtime = time.Now()
	}

	return nil
}

// LoadFile ..
func (vcfg *VCFG) LoadFile(f vio.File) error {

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}

	x := new(VCFG)
	_, err = toml.Decode(string(data), x)
	if err != nil {
		return err
	}
	x.modtime = f.ModTime()

	return vcfg.Merge(x)
}

// Marshal ..
func (vcfg *VCFG) Marshal() ([]byte, error) {
	buf := new(bytes.Buffer)
	enc := toml.NewEncoder(buf)
	// TODO: check if there's a library that can do this now or upload our fork
	enc.SmartMultiline = true
	err := enc.Encode(vcfg)
	return buf.Bytes(), err
}

func (vcfg *VCFG) negate() {

	// ports
	nics := vcfg.Networks
	for _, nic := range nics {
		protocols := map[string]*[]string{"udp": &nic.UDP, "tcp": &nic.TCP, "http": &nic.HTTP, "https": &nic.HTTPS}
		for protocol, portList := range protocols {
			list := *portList
			sort.Strings(list)
			var i int
			for {
				if i >= len(list) {
					break
				}

				if list[i] == "" {
					list = append(list[:i], list[i+1:]...)
					continue
				}

				if strings.HasPrefix(list[i], "!") {
					k := strings.TrimPrefix(list[i], "!")
					list = append(list[:i], list[i+1:]...)

					// cut matching items from the current protocol
					var found = true
					for found {
						found = false
						x := sort.SearchStrings(list, k)
						if x < len(list) && list[x] == k {
							found = true
							list = append(list[:x], list[x+1:]...)
						}
					}

					// cut matching items from similar protocols
					if protocol == "tcp" {

						tcpList := nic.HTTP
						found = true
						for found {
							found = false
							x := sort.SearchStrings(tcpList, k)
							if x < len(tcpList) && tcpList[x] == k {
								found = true
								tcpList = append(tcpList[:x], tcpList[x+1:]...)
							}
						}
						nic.HTTP = tcpList

						tcpList = nic.HTTPS
						found = true
						for found {
							found = false
							x := sort.SearchStrings(tcpList, k)
							if x < len(tcpList) && tcpList[x] == k {
								found = true
								tcpList = append(tcpList[:x], tcpList[x+1:]...)
							}
						}
						nic.HTTPS = tcpList

					}

					continue
				}

				x := sort.SearchStrings(list, list[i])
				if x < i {
					list = append(list[:i], list[i+1:]...)
					continue
				}

				i++
			}
			*portList = list
		}

		if nic.IP == "" || nic.IP == "!" || nic.IP == "disabled" {
			nic.IP = ""
			nic.Mask = ""
			nic.Gateway = ""
			nic.UDP = nil
			nic.TCP = nil
			nic.HTTP = nil
			nic.HTTPS = nil
		} else if nic.IP == "dhcp" {
			nic.Mask = ""
			nic.Gateway = ""
		}
	}

}

func mergeStringArray(x ...[]string) []string {
	all := make([]string, 0)
	for _, z := range x {
		all = append(all, z...)
	}

	return sanitize(all)
}

func mergeStringMap(x ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range x {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func mergeStringArrayExcludingDuplicateValues(x ...[]string) []string {
	all := make([]string, 0)
	done := make(map[string]interface{})

	for _, z := range x {
		for _, y := range z {
			if _, ok := done[y]; !ok {
				all = append(all, y)
				done[y] = nil
			}
		}
	}

	return sanitize(all)
}

func sanitize(all []string) []string {
	delInd := make([]int, 0)

	for j := 0; j < len(all); j++ {
		if strings.HasPrefix(all[j], "~") {
			target := strings.TrimPrefix(all[j], "~")
			for i := j; i >= 0; i-- {
				if all[i] == target {
					delInd = append(delInd, i)
					break
				}
			}
			delInd = append(delInd, j)
		}
	}

	out := make([]string, 0)
	for k, v := range all {
		cont := false
		for _, x := range delInd {
			if x == k {
				cont = true
			}
		}
		if cont {
			continue
		}
		out = append(out, v)
	}

	return out
}

// Merge ..
func Merge(a, b *VCFG) (*VCFG, error) {

	var err error

	// Sysctl
	if a.Sysctl == nil {
		a.Sysctl = b.Sysctl
	} else if b.Sysctl != nil {
		a.Sysctl = mergeStringMap(a.Sysctl, b.Sysctl)
	}

	// Programs
	if a.Programs == nil {
		a.Programs = b.Programs
	} else if b.Programs != nil {

		for k, p := range a.Programs {
			if len(b.Programs) > k {

				// merge b.Programs[k] over p
				envs := mergeStringArray(p.Env, b.Programs[k].Env)
				bstp := mergeStringArray(p.Bootstrap, b.Programs[k].Bootstrap)
				logfiles := mergeStringArrayExcludingDuplicateValues(p.LogFiles, b.Programs[k].LogFiles)

				err = mergo.Merge(&p, &b.Programs[k], mergo.WithOverride)
				if err != nil {
					return nil, err
				}

				p.Env = envs
				p.Bootstrap = bstp
				p.LogFiles = logfiles

				a.Programs[k] = p

			}
		}

		if len(b.Programs) > len(a.Programs) {
			a.Programs = append(a.Programs, b.Programs[len(a.Programs):]...)
		}
	}

	// Logging
	if a.Logging == nil {
		a.Logging = b.Logging
	} else if b.Logging != nil {

		for k, p := range a.Logging {
			if len(b.Logging) > k {
				cfgs := mergeStringArray(p.Config, b.Logging[k].Config)

				err = mergo.Merge(&p, &b.Logging[k], mergo.WithOverride)
				if err != nil {
					return nil, err
				}

				p.Config = cfgs
				a.Logging[k] = p
			}
		}

		if len(b.Logging) > len(a.Logging) {
			a.Logging = append(a.Logging, b.Logging[len(a.Logging):]...)
		}
	}

	// Networks
	if a.Networks == nil {
		a.Networks = b.Networks
	} else if b.Networks != nil {

		for k, n := range a.Networks {

			if len(b.Networks) > k {

				// merge b.Networks[k] over p
				http := mergeStringArrayExcludingDuplicateValues(n.HTTP, b.Networks[k].HTTP)
				https := mergeStringArrayExcludingDuplicateValues(n.HTTPS, b.Networks[k].HTTPS)
				udp := mergeStringArrayExcludingDuplicateValues(n.UDP, b.Networks[k].UDP)
				tcp := mergeStringArrayExcludingDuplicateValues(n.TCP, b.Networks[k].TCP)

				err = mergo.Merge(&n, &b.Networks[k], mergo.WithOverride)
				if err != nil {
					return nil, err
				}

				n.HTTP = http
				n.HTTPS = https
				n.UDP = udp
				n.TCP = tcp

				a.Networks[k] = n
			}

			if len(b.Networks) > len(a.Networks) {
				a.Networks = append(a.Networks, b.Networks[len(a.Networks):]...)
			}
		}

	}

	if a.Logging == nil {
		a.Logging = b.Logging
	} else if b.Logging != nil {
		for k, r := range a.Logging {
			err = mergo.Merge(&r, &b.Logging[k], mergo.WithOverride)
			if err != nil {
				return nil, err
			}
		}

		if len(b.Logging) > len(a.Logging) {
			a.Logging = append(a.Logging, b.Logging[len(a.Logging):]...)
		}
	}

	// System.DNS
	dns := mergeStringArrayExcludingDuplicateValues(a.System.DNS, b.System.DNS)
	// System.NTP
	ntp := mergeStringArrayExcludingDuplicateValues(a.System.NTP, b.System.NTP)

	// System
	err = mergo.Merge(&a.System, &b.System, mergo.WithOverride)
	if err != nil {
		return nil, err
	}

	a.System.DNS = dns
	a.System.NTP = ntp

	// Info
	err = mergo.Merge(&a.Info, &b.Info)
	if err != nil {
		return nil, err
	}

	// VM
	err = mergo.Merge(&a.VM, &b.VM, mergo.WithOverride)
	if err != nil {
		return nil, err
	}

	// NFS
	if a.NFS == nil {
		a.NFS = b.NFS
	} else if b.NFS != nil {

		for k, n := range a.NFS {
			if len(b.NFS) > k {
				err = mergo.Merge(&n, &b.NFS[k], mergo.WithOverride)
				if err != nil {
					return nil, err
				}

				a.NFS[k] = n
			}
		}

		if len(b.NFS) > len(a.NFS) {
			a.NFS = append(a.NFS, b.NFS[len(a.NFS):]...)
		}

	}

	// Routes
	if a.Routing == nil {
		a.Routing = b.Routing
	} else if b.Routing != nil {

		for k, r := range a.Routing {
			if len(b.Routing) > k {
				err = mergo.Merge(&r, &b.Routing[k], mergo.WithOverride)
				if err != nil {
					return nil, err
				}

				a.Routing[k] = r
			}
		}

		if len(b.Routing) > len(a.Routing) {
			a.Routing = append(a.Routing, b.Routing[len(a.Routing):]...)
		}
	}

	return a, nil
}
