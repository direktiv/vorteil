package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/mattn/go-shellwords"
	"github.com/vorteil/vorteil/pkg/flag"
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
		setFlagArgArray(arg)
	}
}

func setFlagArgArray(arg string) {
	if !strings.HasPrefix(arg, "--") {
		return
	}

	elems := strings.Split(arg, "[")
	if len(elems) > 1 {
		elems[1] = strings.Split(elems[1], "]")[0]
		switch elems[0] {
		case "--network":
			tallyRepeatableFlag(&maxNetworkFlags, elems[1])
		case "--program":
			tallyRepeatableFlag(&maxProgramFlags, elems[1])
		case "--nfs":
			tallyRepeatableFlag(&maxNFSFlags, elems[1])
		case "--logging":
			tallyRepeatableFlag(&maxLoggingFlags, elems[1])
		case "--redirect":
			tallyRepeatableFlag(&maxRedirectFlags, elems[1])
		}
	}

}

func tallyRepeatableFlag(v *int, s string) {
	if i, err := strconv.Atoi(s); err == nil && (i+1) > *v {
		*v = i + 1
	}
}

// --sysctl
var sysctlFlag = flag.StringSliceFlag{
	FlagPart: flag.NewFlagPart("sysctl", "add a sysctl key/value tuple", hideFlags),
	Validate: func(f flag.StringSliceFlag) error {
		for _, s := range f.Value {
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
var infoAuthorFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("info.author", "name the author of the app", hideFlags),
	Validate: func(f flag.StringFlag) error {
		overrideVCFG.Info.Author = f.Value
		return nil
	},
}

// --info.date
var infoDateFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("info.date", "date of app's release (YYYY-MM-DD)", hideFlags),
	Validate: func(f flag.StringFlag) error {
		var err error
		if f.Value == "" {
			return nil
		}
		overrideVCFG.Info.Date, err = vcfg.TimestampFromString(f.Value)
		return err
	},
}

// --info.description
var infoDescriptionFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("info.description", "provide a description for the app", hideFlags),
	Validate: func(f flag.StringFlag) error {
		overrideVCFG.Info.Description = f.Value
		return nil
	},
}

// --info.name
var infoNameFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("info.name", "name of the app", hideFlags),
	Validate: func(f flag.StringFlag) error {
		overrideVCFG.Info.Name = f.Value
		return nil
	},
}

// --info.summary
var infoSummaryFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("info.summary", "provide a short summary for the app", hideFlags),
	Validate: func(f flag.StringFlag) error {
		overrideVCFG.Info.Summary = f.Value
		return nil
	},
}

// --info.url
var infoURLFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("info.url", "URL for more information about the app", hideFlags),
	Validate: func(f flag.StringFlag) error {
		var err error
		overrideVCFG.Info.URL, err = vcfg.URLFromString(f.Value)
		return err
	},
}

// --info.version
var infoVersionFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("info.version", "identify the app's version", hideFlags),
	Validate: func(f flag.StringFlag) error {
		overrideVCFG.Info.Version = f.Value
		return nil
	},
}

// --logging.config
var loggingConfigFlag = flag.NStringSliceFlag{
	FlagPart: flag.NewFlagPart("logging[<<N>>].config", "configure app's logging config", hideFlags),
	Total:    &maxLoggingFlags,
	Validate: func(f flag.NStringSliceFlag) error {

		for i := 0; i < *f.Total; i++ {

			if len(f.Value[i]) == 0 {
				continue
			}

			s := f.Value[i]
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
var loggingTypeFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("logging[<<N>>].type", "configure app's logging type", hideFlags),
	Total:    &maxLoggingFlags,
	Validate: func(f flag.NStringFlag) error {
		for i := 0; i < *f.Total; i++ {
			if f.Value[i] == "" {
				continue
			}

			s := f.Value[i]
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
var nfsMountFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("nfs[<<N>>].mount", "configure app's nfs mounts", hideFlags),
	Total:    &maxNFSFlags,
	Validate: func(f flag.NStringFlag) error {

		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			s := f.Value[i]
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
var nfsOptionsFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("nfs[<<N>>].options", "configure app's nfs options", hideFlags),
	Total:    &maxNFSFlags,
	Validate: func(f flag.NStringFlag) error {

		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			s := f.Value[i]
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
var nfsServerFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("nfs[<<N>>].server", "configure app's nfs servers", hideFlags),
	Total:    &maxNFSFlags,
	Validate: func(f flag.NStringFlag) error {

		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			s := f.Value[i]
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
var networkTCPDumpFlag = flag.NBoolFlag{
	FlagPart: flag.NewFlagPart("network[<<N>>].tcpdump", "configure this network to run with tcpdump", hideFlags),
	Total:    &maxNetworkFlags,
	Validate: func(f flag.NBoolFlag) error {
		for i := 0; i < *f.Total; i++ {

			for len(overrideVCFG.Networks) < i+1 {
				overrideVCFG.Networks = append(overrideVCFG.Networks, vcfg.NetworkInterface{})
			}

			val := f.Value[i]
			overrideVCFG.Networks[i].TCPDUMP = val
		}
		return nil
	},
}

// --network.gateway
var networkGatewayFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("network[<<N>>].gateway", "configure app's network gateway", hideFlags),
	Total:    &maxNetworkFlags,
	Validate: func(f flag.NStringFlag) error {

		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			s := f.Value[i]
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
var networkHTTPFlag = flag.NStringSliceFlag{
	FlagPart: flag.NewFlagPart("network[<<N>>].http", "expose http port", hideFlags),
	Total:    &maxNetworkFlags,
	Validate: func(f flag.NStringSliceFlag) error {

		for i := 0; i < *f.Total; i++ {

			if len(f.Value[i]) == 0 {
				continue
			}

			s := f.Value[i]
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
var networkHTTPSFlag = flag.NStringSliceFlag{
	FlagPart: flag.NewFlagPart("network[<<N>>].https", "expose https port", hideFlags),
	Total:    &maxNetworkFlags,
	Validate: func(f flag.NStringSliceFlag) error {

		for i := 0; i < *f.Total; i++ {

			if len(f.Value[i]) == 0 {
				continue
			}

			s := f.Value[i]
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
var networkIPFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("network[<<N>>].ip", "configure app's network IP address", hideFlags),
	Total:    &maxNetworkFlags,
	Validate: func(f flag.NStringFlag) error {

		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			s := f.Value[i]
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
var networkMTUFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("network[<<N>>].mtu", "configure app's network interface MTU", hideFlags),
	Total:    &maxNetworkFlags,
	Validate: func(f flag.NStringFlag) error {

		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			s := f.Value[i]
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
var networkMaskFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("network[<<N>>].mask", "configure app's subnet mask", hideFlags),
	Total:    &maxNetworkFlags,
	Validate: func(f flag.NStringFlag) error {

		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			s := f.Value[i]
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
var networkTCPFlag = flag.NStringSliceFlag{
	FlagPart: flag.NewFlagPart("network[<<N>>].tcp", "expose tcp port", hideFlags),
	Total:    &maxNetworkFlags,
	Validate: func(f flag.NStringSliceFlag) error {

		for i := 0; i < *f.Total; i++ {

			if len(f.Value[i]) == 0 {
				continue
			}

			s := f.Value[i]
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
var networkUDPFlag = flag.NStringSliceFlag{
	FlagPart: flag.NewFlagPart("network[<<N>>].udp", "expose udp port", hideFlags),
	Total:    &maxNetworkFlags,
	Validate: func(f flag.NStringSliceFlag) error {

		for i := 0; i < *f.Total; i++ {

			if len(f.Value[i]) == 0 {
				continue
			}

			s := f.Value[i]
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
var systemKernelArgsFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("system.kernel-args", "", hideFlags),
	Validate: func(f flag.StringFlag) error {
		overrideVCFG.System.KernelArgs = f.Value
		return nil
	},
}

// --system.dns
var systemDNSFlag = flag.StringSliceFlag{
	FlagPart: flag.NewFlagPart("system.dns", "set the DNS server list for the system", hideFlags),
	Validate: func(f flag.StringSliceFlag) error {
		overrideVCFG.System.DNS = f.Value
		return nil
	},
}

// --system.hostname
var systemHostnameFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("system.hostname", "set the hostname for the system", hideFlags),
	Validate: func(f flag.StringFlag) error {
		overrideVCFG.System.Hostname = f.Value
		return nil
	},
}

// --system.filesystem
var systemFilesystemFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("system.filesystem", "set the filesystem format", hideFlags),
	Validate: func(f flag.StringFlag) error {
		overrideVCFG.System.Filesystem = vcfg.Filesystem(f.Value)
		return nil
	},
}

// --system.max-fds
var systemMaxFDsFlag = flag.UintFlag{
	FlagPart: flag.NewFlagPart("system.max-fds", "maximum file descriptors available to app", hideFlags),
	Validate: func(f flag.UintFlag) error {
		overrideVCFG.System.MaxFDs = f.Value
		return nil
	},
}

// --system.output-mode
var systemOutputModeFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("system.output-mode", "specify vm output behaviour mode", hideFlags),
	Validate: func(f flag.StringFlag) error {
		overrideVCFG.System.StdoutMode = vcfg.StdoutModeFromString(f.Value)
		return nil
	},
}

// -- system.user
var systemUserFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("system.user", "name of the non-root user (default: vorteil)", hideFlags),
	Validate: func(f flag.StringFlag) error {
		overrideVCFG.System.User = f.Value
		return nil
	},
}

// a key can have multiple destinations
// key = src, vals = dst
var filesMap = make(map[string][]string)

// --files
var filesFlag = flag.StringSliceFlag{
	FlagPart: flag.NewFlagPart("files", "<src>[@<dst>]   add files from the host filesystem to an existing folder in the virtual machine filesystem (dst defaults to '/')", hideFlags),
	Validate: func(f flag.StringSliceFlag) error {
		for _, v := range f.Value {
			// value should have no more than 2 elements when split by '@'
			x := strings.SplitN(v, "@", 2)

			var src = x[0]
			var dst = "/"

			if len(x) > 1 {
				dst = x[1]
			}

			if _, ok := filesMap[src]; !ok {
				filesMap[src] = make([]string, 0)
			}

			filesMap[src] = append(filesMap[src], dst)
		}
		return nil
	},
}

// --vm.cpus
var vmCPUsFlag = flag.UintFlag{
	FlagPart: flag.NewFlagPart("vm.cpus", "number of cpus to allocate to app", hideFlags),
	Validate: func(f flag.UintFlag) error {
		overrideVCFG.VM.CPUs = f.Value
		return nil
	},
}

// --vm.disk-size
var vmDiskSizeFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("vm.disk-size", "disk image capacity to allocate to app", hideFlags),
	Validate: func(f flag.StringFlag) error {
		var err error
		overrideVCFG.VM.DiskSize, err = vcfg.ParseBytes(f.Value)
		if err != nil {
			return fmt.Errorf("--%s=%s: %v", f.Key,
				f.Value, err)
		}
		return nil
	},
}

// --vm.inodes
var vmInodesFlag = flag.UintFlag{
	FlagPart: flag.NewFlagPart("vm.inodes", "number of inodes to build on disk image", hideFlags),
	Validate: func(f flag.UintFlag) error {
		overrideVCFG.VM.Inodes = vcfg.InodesQuota(f.Value)
		return nil
	},
}

// --vm.kernel
var vmKernelFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("vm.kernel", "kernel to build app on", hideFlags),
	Validate: func(f flag.StringFlag) error {
		if f.Value != "" {
			overrideVCFG.VM.Kernel = f.Value
		}
		return nil
	},
}

// --vm.ram
var vmRAMFlag = flag.StringFlag{
	FlagPart: flag.NewFlagPart("vm.ram", "memory to allocate to app", hideFlags),
	Validate: func(f flag.StringFlag) error {
		var err error
		overrideVCFG.VM.RAM, err = vcfg.ParseBytes(f.Value)
		if err != nil {
			return fmt.Errorf("--%s=%s: %v", f.Key,
				f.Value, err)
		}
		return nil
	},
}

// --program.binary
var programBinaryFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("program[<<N>>].binary", "configure a program binary", hideFlags),
	Total:    &maxProgramFlags,
	Validate: func(f flag.NStringFlag) error {
		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Binary = f.Value[i]
		}

		return nil

	},
}

// --program.privileges
var programPrivilegesFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("program[<<N>>].privilege", "configure program privileges (root, superuser, user)", hideFlags),
	Total:    &maxProgramFlags,
	Validate: func(f flag.NStringFlag) error {

		for i := 0; i < *f.Total; i++ {
			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			val := f.Value[i]
			overrideVCFG.Programs[i].Privilege = vcfg.Privilege(val)
		}

		return nil

	},
}

// --program.env
var programEnvFlag = flag.NStringSliceFlag{
	FlagPart: flag.NewFlagPart("program[<<N>>].env", "configure the environment variables of a program", hideFlags),
	Total:    &maxProgramFlags,
	Validate: func(f flag.NStringSliceFlag) error {
		for i := 0; i < *f.Total; i++ {
			if f.Value[i] == nil {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Env = f.Value[i]
		}

		return nil
	},
}

// --program.cwd
var programCWDFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("program[<<N>>].cwd", "configure the working directory of a program", hideFlags),
	Total:    &maxProgramFlags,
	Validate: func(f flag.NStringFlag) error {
		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Cwd = f.Value[i]
		}
		return nil
	},
}

// --program.logfiles
var programLogFilesFlag = flag.NStringSliceFlag{
	FlagPart: flag.NewFlagPart("program[<<N>>].logfiles", "configure the logfiles of a program", hideFlags),
	Total:    &maxProgramFlags,
	Validate: func(f flag.NStringSliceFlag) error {
		for i := 0; i < *f.Total; i++ {
			if f.Value[i] == nil {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].LogFiles = f.Value[i]
		}

		return nil
	},
}

// --program.bootstrap
var programBootstrapFlag = flag.NStringSliceFlag{
	FlagPart: flag.NewFlagPart("program[<<N>>].bootstrap", "configure the bootstrap parameters of a program", hideFlags),
	Total:    &maxProgramFlags,
	Validate: func(f flag.NStringSliceFlag) error {
		for i := 0; i < *f.Total; i++ {
			if f.Value[i] == nil {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Bootstrap = f.Value[i]
		}

		return nil
	},
}

// --program.stdout
var programStdoutFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("program[<<N>>].stdout", "configure programs stdout", hideFlags),
	Total:    &maxProgramFlags,
	Validate: func(f flag.NStringFlag) error {

		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Stdout = f.Value[i]
		}

		return nil

	},
}

// --program.stderr
var programStderrFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("program[<<N>>].stderr", "configure programs stderr", hideFlags),
	Total:    &maxProgramFlags,
	Validate: func(f flag.NStringFlag) error {

		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Stderr = f.Value[i]
		}

		return nil

	},
}

// --program.strace
var programStraceFlag = flag.NBoolFlag{
	FlagPart: flag.NewFlagPart("program[<<N>>].strace", "configure the program to run with strace", hideFlags),
	Total:    &maxProgramFlags,
	Validate: func(f flag.NBoolFlag) error {
		for i := 0; i < *f.Total; i++ {

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			val := f.Value[i]
			overrideVCFG.Programs[i].Strace = val
		}
		return nil
	},
}

// --program.args
var programArgsFlag = flag.NStringFlag{
	FlagPart: flag.NewFlagPart("program[<<N>>].args", "configure programs args", hideFlags),
	Total:    &maxProgramFlags,
	Validate: func(f flag.NStringFlag) error {

		for i := 0; i < *f.Total; i++ {

			if f.Value[i] == "" {
				continue
			}

			for len(overrideVCFG.Programs) < i+1 {
				overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
			}

			overrideVCFG.Programs[i].Args = f.Value[i]
		}

		return nil

	},
}

var vcfgFlags = flag.FlagsList{
	&vmCPUsFlag, &vmDiskSizeFlag, &vmInodesFlag, &vmKernelFlag, &vmRAMFlag,
	&filesFlag, &infoAuthorFlag, &infoDateFlag, &infoDescriptionFlag,
	&infoNameFlag, &infoSummaryFlag, &infoURLFlag, &infoVersionFlag,
	&networkIPFlag, &networkMaskFlag, &networkGatewayFlag, &networkUDPFlag,
	&networkTCPFlag, &networkHTTPFlag, &networkHTTPSFlag, &networkMTUFlag,
	&networkTCPDumpFlag, &loggingConfigFlag, &loggingTypeFlag, &nfsMountFlag,
	&nfsServerFlag, &nfsOptionsFlag, &systemKernelArgsFlag, &systemDNSFlag,
	&systemHostnameFlag, &systemFilesystemFlag, &systemMaxFDsFlag,
	&systemOutputModeFlag, &systemUserFlag, &programBinaryFlag,
	&programPrivilegesFlag, &programArgsFlag, &programStdoutFlag,
	&programStderrFlag, &programLogFilesFlag, &programBootstrapFlag,
	&programEnvFlag, &programCWDFlag, &programStraceFlag, &sysctlFlag,
}
