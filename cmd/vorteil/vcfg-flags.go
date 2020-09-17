package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/mattn/go-shellwords"
	"github.com/spf13/pflag"
	"github.com/vorteil/vorteil/pkg/vcfg"
)

var (
	hideFlags        = false
	maxRedirectFlags int
	maxNetworkFlags  int
	maxProgramFlags  int
	maxNFSFlags      int
	maxLoggingFlags  int
)

func init() {
	// fixes a flag parsing issue
	if runtime.GOOS == "windows" {
		for i := range os.Args {
			splitQuote := strings.Split(os.Args[i], "\"")
			_, err := os.Stat(splitQuote[0])
			if err == nil {
				os.Args[i] = filepath.ToSlash(splitQuote[0])
			}
		}
	}

	args, err := shellwords.Parse(strings.Join(os.Args, " "))
	if err != nil {
		// panic(err)
	}

	for _, arg := range args {
		if strings.HasPrefix(arg, "--") {
			elems := strings.Split(arg, "[")
			if len(elems) > 1 {
				elems[1] = strings.Split(elems[1], "]")[0]
				switch elems[0] {
				case "--network":
					i, err := strconv.Atoi(elems[1])
					if err != nil {
						continue
					}
					i++
					if i > maxNetworkFlags {
						maxNetworkFlags = i
					}
				case "--program":
					// check if root flag is being parsed
					i, err := strconv.Atoi(elems[1])
					if err != nil {
						continue
					}
					i++
					if i > maxProgramFlags {
						maxProgramFlags = i
					}
				case "--nfs":
					i, err := strconv.Atoi(elems[1])
					if err != nil {
						continue
					}
					i++
					if i > maxNFSFlags {
						maxNFSFlags = i
					}
				case "--logging":
					i, err := strconv.Atoi(elems[1])
					if err != nil {
						continue
					}
					i++
					if i > maxLoggingFlags {
						maxLoggingFlags = i
					}
				case "--redirect":
					i, err := strconv.Atoi(elems[1])
					if err != nil {
						continue
					}
					i++
					if i > maxRedirectFlags {
						maxRedirectFlags = i
					}
				}
			}
		}
	}
}

type flag interface {
	FlagKey() string
	FlagShort() string
	FlagUsage() string
	FlagValidate() error
	AddTo(flagSet *pflag.FlagSet)
	AddUnhiddenTo(flagSet *pflag.FlagSet)
}

type flagsList []flag

func (f flagsList) AddTo(flagSet *pflag.FlagSet) {
	for _, x := range f {
		x.AddTo(flagSet)
	}
}

func (f flagsList) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	for _, x := range f {
		x.AddUnhiddenTo(flagSet)
	}
}

func (f flagsList) Validate() error {
	for _, x := range f {
		err := x.FlagValidate()
		if err != nil {
			fmt.Println(x.FlagKey())
			return err
		}
	}
	return nil
}

type flagPart struct {
	key    string
	short  string
	usage  string
	hidden bool
}

func (p flagPart) FlagKey() string {
	return p.key
}

func (p flagPart) FlagShort() string {
	return p.short
}

func (p flagPart) FlagUsage() string {
	return p.usage
}

type boolFlag struct {
	flagPart
	value    bool
	validate func(value boolFlag) error
}

func (f *boolFlag) AddTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.BoolVar(&f.value, f.key, f.value, f.usage)
	} else {
		flagSet.BoolVarP(&f.value, f.key, f.short, f.value, f.usage)
	}
	if f.hidden {
		flag := flagSet.Lookup(f.key)
		flag.Hidden = true
	}
}

func (f *boolFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.BoolVar(&f.value, f.key, f.value, f.usage)
	} else {
		flagSet.BoolVarP(&f.value, f.key, f.short, f.value, f.usage)
	}
}

func (f boolFlag) FlagValidate() error {
	if f.validate == nil {
		return nil
	}
	return f.validate(f)
}

type nBoolFlag struct {
	flagPart
	total    *int
	void     bool
	value    []bool
	validate func(f nBoolFlag) error
}

func (f *nBoolFlag) AddTo(flagSet *pflag.FlagSet) {

	if f.value == nil {
		f.value = make([]bool, *f.total, *f.total)
	}

	key := strings.Replace(f.key, "<<N>>", "i", -1)
	flagSet.BoolVar(&f.void, key, f.void, f.usage)
	if f.hidden {
		flag := flagSet.Lookup(key)
		flag.Hidden = true
	}

	for i := 0; i < *f.total; i++ {
		key = f.nKey(i)
		flagSet.BoolVar(&f.value[i], key, f.value[i], f.usage)
		flagSet.MarkHidden(key)
		if f.hidden {
			flag := flagSet.Lookup(key)
			flag.Hidden = true
		}
	}

}

func (f nBoolFlag) nKey(n int) string {
	return strings.Replace(f.key, "<<N>>", fmt.Sprintf("%d", n), -1)
}

func (f *nBoolFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {

	if f.value == nil {
		f.value = make([]bool, *f.total, *f.total)
	}

	key := strings.Replace(f.key, "<<N>>", "i", -1)
	flagSet.BoolVar(&f.void, key, f.void, f.usage)

	for i := 0; i < *f.total; i++ {
		key = f.nKey(i)
		flagSet.BoolVar(&f.value[i], key, f.value[i], f.usage)
		flagSet.MarkHidden(key)
	}

}

func (f nBoolFlag) FlagValidate() error {

	if f.void {
		key := strings.Replace(f.key, "<<N>>", "i", -1)
		suggest := strings.Replace(f.key, "<<N>>", "0", -1)
		return fmt.Errorf("unknown flag: --%s (substitute 'N' for 0-%d, e.g. %s)", key, f.total, suggest)
	}

	if f.validate == nil {
		return nil
	}

	return f.validate(f)

}

type nStringFlag struct {
	flagPart
	total    *int
	void     string
	value    []string
	validate func(f nStringFlag) error
}

func (f *nStringFlag) AddTo(flagSet *pflag.FlagSet) {
	if f.value == nil {
		f.value = make([]string, *f.total, *f.total)
	}

	key := strings.Replace(f.key, "<<N>>", "i", -1)
	flagSet.StringVar(&f.void, key, f.void, f.usage)
	if f.hidden {
		flag := flagSet.Lookup(key)
		flag.Hidden = true
	}
	for i := 0; i < *f.total; i++ {
		key = f.nKey(i)
		flagSet.StringVar(&f.value[i], key, f.value[i], f.usage)
		flagSet.MarkHidden(key)
		if f.hidden {
			flag := flagSet.Lookup(key)
			flag.Hidden = true
		}
	}

}

func (f *nStringFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {

	if f.value == nil {
		f.value = make([]string, *f.total, *f.total)
	}

	key := strings.Replace(f.key, "<<N>>", "i", -1)
	flagSet.StringVar(&f.void, key, f.void, f.usage)

	for i := 0; i < *f.total; i++ {
		key = f.nKey(i)
		flagSet.StringVar(&f.value[i], key, f.value[i], f.usage)
		flagSet.MarkHidden(key)
	}

}

func (f nStringFlag) FlagValidate() error {

	if f.void != "" {
		key := strings.Replace(f.key, "<<N>>", "i", -1)
		suggest := strings.Replace(f.key, "<<N>>", "0", -1)
		return fmt.Errorf("unknown flag: --%s (substitute 'N' for 0-%d, e.g. %s)", key, f.total, suggest)
	}

	if f.validate == nil {
		return nil
	}

	return f.validate(f)

}

func (f nStringFlag) nKey(n int) string {
	return strings.Replace(f.key, "<<N>>", fmt.Sprintf("%d", n), -1)
}

type nStringSliceFlag struct {
	flagPart
	total    *int
	void     []string
	value    [][]string
	validate func(f nStringSliceFlag) error
}

func (f *nStringSliceFlag) AddTo(flagSet *pflag.FlagSet) {

	if f.value == nil {
		f.value = make([][]string, *f.total, *f.total)
	}

	key := strings.Replace(f.key, "<<N>>", "i", -1)
	flagSet.StringSliceVar(&f.void, key, f.void, f.usage)
	if f.hidden {
		flag := flagSet.Lookup(key)
		flag.Hidden = true
	}

	for i := 0; i < *f.total; i++ {
		key = f.nKey(i)
		flagSet.StringSliceVar(&f.value[i], key, f.value[i], f.usage)
		flagSet.MarkHidden(key)
		if f.hidden {
			flag := flagSet.Lookup(key)
			flag.Hidden = true
		}
	}

}

func (f *nStringSliceFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {

	if f.value == nil {
		f.value = make([][]string, *f.total, *f.total)
	}

	key := strings.Replace(f.key, "<<N>>", "i", -1)
	flagSet.StringSliceVar(&f.void, key, f.void, f.usage)

	for i := 0; i < *f.total; i++ {
		key = f.nKey(i)
		flagSet.StringSliceVar(&f.value[i], key, f.value[i], f.usage)
		flagSet.MarkHidden(key)
	}

}

func (f nStringSliceFlag) FlagValidate() error {

	if len(f.void) != 0 {
		key := strings.Replace(f.key, "<<N>>", "i", -1)
		suggest := strings.Replace(f.key, "<<N>>", "0", -1)
		return fmt.Errorf("unknown flag: --%s (substitute 'N' for 0-%d, e.g. %s)", key, f.total, suggest)
	}

	if f.validate == nil {
		return nil
	}

	return f.validate(f)

}

func (f nStringSliceFlag) nKey(n int) string {
	return strings.Replace(f.key, "<<N>>", fmt.Sprintf("%d", n), -1)
}

type stringFlag struct {
	flagPart
	value    string
	validate func(value stringFlag) error
}

func (f *stringFlag) AddTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.StringVar(&f.value, f.key, f.value, f.usage)
	} else {
		flagSet.StringVarP(&f.value, f.key, f.short, f.value, f.usage)
	}
	if f.hidden {
		flag := flagSet.Lookup(f.key)
		flag.Hidden = true
	}
}

func (f *stringFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.StringVar(&f.value, f.key, f.value, f.usage)
	} else {
		flagSet.StringVarP(&f.value, f.key, f.short, f.value, f.usage)
	}
}

func (f stringFlag) FlagValidate() error {
	if f.validate == nil {
		return nil
	}
	return f.validate(f)
}

type stringSliceFlag struct {
	flagPart
	value    []string
	validate func(f stringSliceFlag) error
}

func (f *stringSliceFlag) AddTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.StringSliceVar(&f.value, f.key, f.value, f.usage)
	} else {
		flagSet.StringSliceVarP(&f.value, f.key, f.short, f.value, f.usage)
	}
	if f.hidden {
		flag := flagSet.Lookup(f.key)
		flag.Hidden = true
	}
}

func (f *stringSliceFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.StringSliceVar(&f.value, f.key, f.value, f.usage)
	} else {
		flagSet.StringSliceVarP(&f.value, f.key, f.short, f.value, f.usage)
	}
}

func (f stringSliceFlag) FlagValidate() error {
	if f.validate == nil {
		return nil
	}
	return f.validate(f)
}

type uintFlag struct {
	flagPart
	value    uint
	validate func(f uintFlag) error
}

func (f *uintFlag) AddTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.UintVar(&f.value, f.key, f.value, f.usage)
	} else {
		flagSet.UintVarP(&f.value, f.key, f.short, f.value, f.usage)
	}
	if f.hidden {
		flag := flagSet.Lookup(f.key)
		flag.Hidden = true
	}
}

func (f *uintFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.UintVar(&f.value, f.key, f.value, f.usage)
	} else {
		flagSet.UintVarP(&f.value, f.key, f.short, f.value, f.usage)
	}
}

func (f uintFlag) FlagValidate() error {
	if f.validate == nil {
		return nil
	}
	return f.validate(f)
}

// --sysctl
var sysctlFlag = stringSliceFlag{
	flagPart: flagPart{
		key:    "sysctl",
		usage:  "add a sysctl key/value tuple",
		hidden: hideFlags,
	},
	validate: func(f stringSliceFlag) error {
		for _, s := range f.value {
			x := strings.Split(s, "=")

			if len(x) < 2 {
				log.Warnf("invalid sysctl tuple '%s'", s)
			}

			if overrideVCFG.Sysctl == nil {
				overrideVCFG.Sysctl = make(map[string]string)
			}

			overrideVCFG.Sysctl[x[0]] = x[1]
		}

		return nil
	},
}

// --info.author
var infoAuthorFlag = stringFlag{
	flagPart: flagPart{
		key:    "info.author",
		usage:  "name the author of the app",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		overrideVCFG.Info.Author = f.value
		return nil
	},
}

// --info.date
var infoDateFlag = stringFlag{
	flagPart: flagPart{
		key:    "info.date",
		usage:  "date of app's release (YYYY-MM-DD)",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		var err error
		if f.value == "" {
			return nil
		}
		overrideVCFG.Info.Date, err = vcfg.TimestampFromString(f.value)
		return err
	},
}

// --info.description
var infoDescriptionFlag = stringFlag{
	flagPart: flagPart{
		key:    "info.description",
		usage:  "provide a description for the app",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		overrideVCFG.Info.Description = f.value
		return nil
	},
}

// --info.name
var infoNameFlag = stringFlag{
	flagPart: flagPart{
		key:    "info.name",
		usage:  "name the app",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		overrideVCFG.Info.Name = f.value
		return nil
	},
}

// --info.summary
var infoSummaryFlag = stringFlag{
	flagPart: flagPart{
		key:    "info.summary",
		usage:  "provide a short summary for the app",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		overrideVCFG.Info.Summary = f.value
		return nil
	},
}

// --info.url
var infoURLFlag = stringFlag{
	flagPart: flagPart{
		key:    "info.url",
		usage:  "URL for more information about the app",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		var err error
		overrideVCFG.Info.URL, err = vcfg.URLFromString(f.value)
		return err
	},
}

// --info.version
var infoVersionFlag = stringFlag{
	flagPart: flagPart{
		key:    "info.version",
		usage:  "identify the app's version",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		overrideVCFG.Info.Version = f.value
		return nil
	},
}

// --logging.config
var loggingConfigFlag = nStringSliceFlag{
	flagPart: flagPart{
		key:    "logging[<<N>>].config",
		usage:  "configure app's logging config",
		hidden: hideFlags,
	},
	total: &maxLoggingFlags,
	validate: func(f nStringSliceFlag) error {

		for i := 0; i < *f.total; i++ {

			if len(f.value[i]) == 0 {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.Logging) < i+1 {
				overrideVCFG.Logging = append(overrideVCFG.Logging, vcfg.Logging{})
			}

			logging := &overrideVCFG.Logging[i]
			if logging.Config == nil {
				logging.Config = make([]string, 0)
			}
			logging.Config = append(logging.Config, s...)
		}

		return nil
	},
}

// --logging.type
var loggingTypeFlag = nStringFlag{
	flagPart: flagPart{
		key:    "logging[<<N>>].type",
		usage:  "configure app's logging type",
		hidden: hideFlags,
	},
	total: &maxLoggingFlags,
	validate: func(f nStringFlag) error {
		for i := 0; i < *f.total; i++ {
			if f.value[i] == "" {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.Logging) < i+1 {
				overrideVCFG.Logging = append(overrideVCFG.Logging, vcfg.Logging{})
			}
			logging := &overrideVCFG.Logging[i]
			logging.Type = s
		}

		return nil
	},
}

// --nfs.mount
var nfsMountFlag = nStringFlag{
	flagPart: flagPart{
		key:    "nfs[<<N>>].mount",
		usage:  "configure app's nfs mounts",
		hidden: hideFlags,
	},
	total: &maxNFSFlags,
	validate: func(f nStringFlag) error {

		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.NFS) < i+1 {
				overrideVCFG.NFS = append(overrideVCFG.NFS, vcfg.NFSSettings{})
			}
			mount := &overrideVCFG.NFS[i]
			mount.MountPoint = s
		}

		return nil

	},
}

// --nfs.options
var nfsOptionsFlag = nStringFlag{
	flagPart: flagPart{
		key:    "nfs[<<N>>].options",
		usage:  "configure app's nfs options",
		hidden: hideFlags,
	},
	total: &maxNFSFlags,
	validate: func(f nStringFlag) error {

		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.NFS) < i+1 {
				overrideVCFG.NFS = append(overrideVCFG.NFS, vcfg.NFSSettings{})
			}
			mount := &overrideVCFG.NFS[i]
			mount.Arguments = s
		}

		return nil

	},
}

// --nfs.server
var nfsServerFlag = nStringFlag{
	flagPart: flagPart{
		key:    "nfs[<<N>>].server",
		usage:  "configure app's nfs servers",
		hidden: hideFlags,
	},
	total: &maxNFSFlags,
	validate: func(f nStringFlag) error {

		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.NFS) < i+1 {
				overrideVCFG.NFS = append(overrideVCFG.NFS, vcfg.NFSSettings{})
			}
			mount := &overrideVCFG.NFS[i]
			mount.Server = s
		}

		return nil

	},
}

// --network.tcpdump
var networkTCPDumpFlag = nBoolFlag{
	flagPart: flagPart{
		key:    "network[<<N>>].tcpdump",
		usage:  "configure this network to run with tcpdump",
		hidden: hideFlags,
	},
	total: &maxNetworkFlags,
	validate: func(f nBoolFlag) error {
		for i := 0; i < *f.total; i++ {

			for len(overrideVCFG.Networks) < i+1 {
				overrideVCFG.Networks = append(overrideVCFG.Networks, vcfg.NetworkInterface{})
			}

			val := f.value[i]
			overrideVCFG.Networks[i].TCPDUMP = val
		}
		return nil
	},
}

// --network.gateway
var networkGatewayFlag = nStringFlag{
	flagPart: flagPart{
		key:    "network[<<N>>].gateway",
		usage:  "configure app's network gateway",
		hidden: hideFlags,
	},
	total: &maxNetworkFlags,
	validate: func(f nStringFlag) error {

		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.Networks) < i+1 {
				overrideVCFG.Networks = append(overrideVCFG.Networks, vcfg.NetworkInterface{IP: "dhcp"})
			}
			nic := &overrideVCFG.Networks[i]
			nic.Gateway = s
		}

		return nil

	},
}

// --network.http
var networkHTTPFlag = nStringSliceFlag{
	flagPart: flagPart{
		key:    "network[<<N>>].http",
		usage:  "expose http port",
		hidden: hideFlags,
	},
	total: &maxNetworkFlags,
	validate: func(f nStringSliceFlag) error {

		for i := 0; i < *f.total; i++ {

			if len(f.value[i]) == 0 {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.Networks) < i+1 {
				overrideVCFG.Networks = append(overrideVCFG.Networks, vcfg.NetworkInterface{IP: "dhcp"})
			}
			nic := &overrideVCFG.Networks[i]
			nic.HTTP = s
		}

		return nil

	},
}

// --network.http
var networkHTTPSFlag = nStringSliceFlag{
	flagPart: flagPart{
		key:    "network[<<N>>].https",
		usage:  "expose https port",
		hidden: hideFlags,
	},
	total: &maxNetworkFlags,
	validate: func(f nStringSliceFlag) error {

		for i := 0; i < *f.total; i++ {

			if len(f.value[i]) == 0 {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.Networks) < i+1 {
				overrideVCFG.Networks = append(overrideVCFG.Networks, vcfg.NetworkInterface{IP: "dhcp"})
			}
			nic := &overrideVCFG.Networks[i]
			nic.HTTPS = s
		}

		return nil

	},
}

// --network.ip
var networkIPFlag = nStringFlag{
	flagPart: flagPart{
		key:    "network[<<N>>].ip",
		usage:  "configure app's network IP address",
		hidden: hideFlags,
	},
	total: &maxNetworkFlags,
	validate: func(f nStringFlag) error {

		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.Networks) < i+1 {
				overrideVCFG.Networks = append(overrideVCFG.Networks, vcfg.NetworkInterface{IP: "dhcp"})
			}
			nic := &overrideVCFG.Networks[i]
			nic.IP = s
		}

		return nil

	},
}

// --network.mtu
var networkMTUFlag = nStringFlag{
	flagPart: flagPart{
		key:    "network[<<N>>].mtu",
		usage:  "configure app's network interface MTU",
		hidden: hideFlags,
	},
	total: &maxNetworkFlags,
	validate: func(f nStringFlag) error {

		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			s := f.value[i]
			x, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return err
			}

			for len(overrideVCFG.Networks) < i+1 {
				overrideVCFG.Networks = append(overrideVCFG.Networks, vcfg.NetworkInterface{IP: "dhcp"})
			}
			nic := &overrideVCFG.Networks[i]
			nic.MTU = uint(x)
		}

		return nil

	},
}

// --network.mask
var networkMaskFlag = nStringFlag{
	flagPart: flagPart{
		key:    "network[<<N>>].mask",
		usage:  "configure app's subnet mask",
		hidden: hideFlags,
	},
	total: &maxNetworkFlags,
	validate: func(f nStringFlag) error {

		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.Networks) < i+1 {
				overrideVCFG.Networks = append(overrideVCFG.Networks, vcfg.NetworkInterface{IP: "dhcp"})
			}
			nic := &overrideVCFG.Networks[i]
			nic.Mask = s
		}

		return nil

	},
}

// --network.tcp
var networkTCPFlag = nStringSliceFlag{
	flagPart: flagPart{
		key:    "network[<<N>>].tcp",
		usage:  "expose tcp port",
		hidden: hideFlags,
	},
	total: &maxNetworkFlags,
	validate: func(f nStringSliceFlag) error {

		for i := 0; i < *f.total; i++ {

			if len(f.value[i]) == 0 {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.Networks) < i+1 {
				overrideVCFG.Networks = append(overrideVCFG.Networks, vcfg.NetworkInterface{IP: "dhcp"})
			}
			nic := &overrideVCFG.Networks[i]
			nic.TCP = s
		}

		return nil

	},
}

// --network.udp
var networkUDPFlag = nStringSliceFlag{
	flagPart: flagPart{
		key:    "network[<<N>>].udp",
		usage:  "expose udp port",
		hidden: hideFlags,
	},
	total: &maxNetworkFlags,
	validate: func(f nStringSliceFlag) error {

		for i := 0; i < *f.total; i++ {

			if len(f.value[i]) == 0 {
				continue
			}

			s := f.value[i]
			for len(overrideVCFG.Networks) < i+1 {
				overrideVCFG.Networks = append(overrideVCFG.Networks, vcfg.NetworkInterface{IP: "dhcp"})
			}
			nic := &overrideVCFG.Networks[i]
			nic.UDP = s
		}

		return nil

	},
}

// --system.kernel-args
var systemKernelArgsFlag = stringFlag{
	flagPart: flagPart{
		key:    "system.kernel-args",
		usage:  "",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		overrideVCFG.System.KernelArgs = f.value
		return nil
	},
}

// --system.dns
var systemDNSFlag = stringSliceFlag{
	flagPart: flagPart{
		key:    "system.dns",
		usage:  "set the DNS server list for the system",
		hidden: hideFlags,
	},
	validate: func(f stringSliceFlag) error {
		overrideVCFG.System.DNS = f.value
		return nil
	},
}

// --system.hostname
var systemHostnameFlag = stringFlag{
	flagPart: flagPart{
		key:    "system.hostname",
		usage:  "set the hostname for the system",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		overrideVCFG.System.Hostname = f.value
		return nil
	},
}

// --system.filesystem
var systemFilesystemFlag = stringFlag{
	flagPart: flagPart{
		key:    "system.filesystem",
		usage:  "set the filesystem",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		overrideVCFG.System.Filesystem = vcfg.Filesystem(f.value)
		return nil
	},
}

// --system.max-fds
var systemMaxFDsFlag = uintFlag{
	flagPart: flagPart{
		key:    "system.max-fds",
		usage:  "maximum file descriptors available to app",
		hidden: hideFlags,
	},
	validate: func(f uintFlag) error {
		overrideVCFG.System.MaxFDs = f.value
		return nil
	},
}

// --system.output-mode
var systemOutputModeFlag = stringFlag{
	flagPart: flagPart{
		key:    "system.output-mode",
		usage:  "specify vm output behaviour mode",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		overrideVCFG.System.StdoutMode = vcfg.StdoutModeFromString(f.value)
		return nil
	},
}

// -- system.user
var systemUserFlag = stringFlag{
	flagPart: flagPart{
		key:    "system.user",
		usage:  "name of the non-root user (default: vorteil)",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		overrideVCFG.System.User = f.value
		return nil
	},
}

// --vm.cpus
var vmCPUsFlag = uintFlag{
	flagPart: flagPart{
		key:    "vm.cpus",
		usage:  "number of cpus to allocate to app",
		hidden: hideFlags,
	},
	validate: func(f uintFlag) error {
		overrideVCFG.VM.CPUs = f.value
		return nil
	},
}

// --vm.disk-size
var vmDiskSizeFlag = stringFlag{
	flagPart: flagPart{
		key:    "vm.disk-size",
		usage:  "disk image capacity to allocate to app",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		var err error
		overrideVCFG.VM.DiskSize, err = vcfg.ParseBytes(f.value)
		if err != nil {
			return fmt.Errorf("--%s=%s: %v", f.key,
				f.value, err)
		}
		return nil
	},
}

// --vm.inodes
var vmInodesFlag = uintFlag{
	flagPart: flagPart{
		key:    "vm.inodes",
		usage:  "number of inodes to build on disk image",
		hidden: hideFlags,
	},
	validate: func(f uintFlag) error {
		overrideVCFG.VM.Inodes = vcfg.InodesQuota(f.value)
		return nil
	},
}

// --vm.kernel
var vmKernelFlag = stringFlag{
	flagPart: flagPart{
		key:    "vm.kernel",
		usage:  "kernel to build app on",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		if f.value != "" {
			overrideVCFG.VM.Kernel = f.value
		}
		return nil
	},
}

// --vm.ram
var vmRAMFlag = stringFlag{
	flagPart: flagPart{
		key:    "vm.ram",
		usage:  "memory in MiB to allocate to app",
		hidden: hideFlags,
	},
	validate: func(f stringFlag) error {
		var err error
		overrideVCFG.VM.RAM, err = vcfg.ParseBytes(f.value)
		if err != nil {
			return fmt.Errorf("--%s=%s: %v", f.key,
				f.value, err)
		}
		return nil
	},
}

// --program.binary
var programBinaryFlag = nStringFlag{
	flagPart: flagPart{
		key:    "program[<<N>>].binary",
		usage:  "configure a program binary",
		hidden: hideFlags,
	},
	total: &maxProgramFlags,
	validate: func(f nStringFlag) error {
		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Binary = f.value[i]
		}

		return nil

	},
}

// --program.privileges
var programPrivilegesFlag = nStringFlag{
	flagPart: flagPart{
		key:    "program[<<N>>].privilege",
		usage:  "configure program privileges (root, superuser, user)",
		hidden: hideFlags,
	},
	total: &maxProgramFlags,
	validate: func(f nStringFlag) error {

		for i := 0; i < *f.total; i++ {
			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			val := f.value[i]
			overrideVCFG.Programs[i].Privilege = vcfg.Privilege(val)
		}

		return nil

	},
}

// --program.env
var programEnvFlag = nStringSliceFlag{
	flagPart: flagPart{
		key:    "program[<<N>>].env",
		usage:  "configure the environment variables of a program",
		hidden: hideFlags,
	},
	total: &maxProgramFlags,
	validate: func(f nStringSliceFlag) error {
		for i := 0; i < *f.total; i++ {
			if f.value[i] == nil {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Env = f.value[i]
		}

		return nil
	},
}

// --program.cwd
var programCWDFlag = nStringFlag{
	flagPart: flagPart{
		key:    "program[<<N>>].cwd",
		usage:  "configure the working directory of a program",
		hidden: hideFlags,
	},
	total: &maxProgramFlags,
	validate: func(f nStringFlag) error {
		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Cwd = f.value[i]
		}
		return nil
	},
}

// --program.logfiles
var programLogFilesFlag = nStringSliceFlag{
	flagPart: flagPart{
		key:    "program[<<N>>].logfiles",
		usage:  "configure the logfiles of a program",
		hidden: hideFlags,
	},
	total: &maxProgramFlags,
	validate: func(f nStringSliceFlag) error {
		for i := 0; i < *f.total; i++ {
			if f.value[i] == nil {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].LogFiles = f.value[i]
		}

		return nil
	},
}

// --program.bootstrap
var programBootstrapFlag = nStringSliceFlag{
	flagPart: flagPart{
		key:    "program[<<N>>].bootstrap",
		usage:  "configure the bootstrap parameters of a program",
		hidden: hideFlags,
	},
	total: &maxProgramFlags,
	validate: func(f nStringSliceFlag) error {
		for i := 0; i < *f.total; i++ {
			if f.value[i] == nil {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Bootstrap = f.value[i]
		}

		return nil
	},
}

// --program.stdout
var programStdoutFlag = nStringFlag{
	flagPart: flagPart{
		key:    "program[<<N>>].stdout",
		usage:  "configure programs stdout",
		hidden: hideFlags,
	},
	total: &maxProgramFlags,
	validate: func(f nStringFlag) error {

		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Stdout = f.value[i]
		}

		return nil

	},
}

// --program.stderr
var programStderrFlag = nStringFlag{
	flagPart: flagPart{
		key:    "program[<<N>>].stderr",
		usage:  "configure programs stderr",
		hidden: hideFlags,
	},
	total: &maxProgramFlags,
	validate: func(f nStringFlag) error {

		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Stderr = f.value[i]
		}

		return nil

	},
}

// --program.strace
var programStraceFlag = nBoolFlag{
	flagPart: flagPart{
		key:    "program[<<N>>].strace",
		usage:  "configure the program to run with strace",
		hidden: hideFlags,
	},
	total: &maxProgramFlags,
	validate: func(f nBoolFlag) error {
		for i := 0; i < *f.total; i++ {

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			val := f.value[i]
			overrideVCFG.Programs[i].Strace = val
		}
		return nil
	},
}

// --program.args
var programArgsFlag = nStringFlag{
	flagPart: flagPart{
		key:    "program[<<N>>].args",
		usage:  "configure programs args",
		hidden: hideFlags,
	},
	total: &maxProgramFlags,
	validate: func(f nStringFlag) error {

		for i := 0; i < *f.total; i++ {

			if f.value[i] == "" {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Args = f.value[i]
		}

		return nil

	},
}

var vcfgFlags = flagsList{
	&vmCPUsFlag,
	&vmDiskSizeFlag,
	&vmInodesFlag,
	&vmKernelFlag,
	&vmRAMFlag,
	&infoAuthorFlag,
	&infoDateFlag,
	&infoDescriptionFlag,
	&infoNameFlag,
	&infoSummaryFlag,
	&infoURLFlag,
	&infoVersionFlag,
	&networkIPFlag,
	&networkMaskFlag,
	&networkGatewayFlag,
	&networkUDPFlag,
	&networkTCPFlag,
	&networkHTTPFlag,
	&networkHTTPSFlag,
	&networkMTUFlag,
	&networkTCPDumpFlag,
	&loggingConfigFlag,
	&loggingTypeFlag,
	&nfsMountFlag,
	&nfsServerFlag,
	&nfsOptionsFlag,
	&systemKernelArgsFlag,
	&systemDNSFlag,
	&systemHostnameFlag,
	&systemFilesystemFlag,
	&systemMaxFDsFlag,
	&systemOutputModeFlag,
	&systemUserFlag,
	&programBinaryFlag,
	&programPrivilegesFlag,
	&programArgsFlag,
	&programStdoutFlag,
	&programStderrFlag,
	&programLogFilesFlag,
	&programBootstrapFlag,
	&programEnvFlag,
	&programCWDFlag,
	&programStraceFlag,
	&sysctlFlag,
}
