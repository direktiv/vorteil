package cli

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

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
var sysctlFlag = flag.NewStringSliceFlag("sysctl", "add a sysctl key/value tuple", hideFlags, sysctlFlagValidator)
var sysctlFlagValidator = func(f flag.StringSliceFlag) error {
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
}

// --info.author
var infoAuthorFlag = flag.NewStringFlag("info.author", "name the author of the app", hideFlags, infoAuthorFlagValidator)
var infoAuthorFlagValidator = func(f flag.StringFlag) error {
	overrideVCFG.Info.Author = f.Value
	return nil
}

// --info.date
var infoDateFlag = flag.NewStringFlag("info.date", "date of app's release (YYYY-MM-DD)", hideFlags, infoDateFlagValidator)
var infoDateFlagValidator = func(f flag.StringFlag) error {
	var err error
	if f.Value == "" {
		return nil
	}
	overrideVCFG.Info.Date, err = vcfg.TimestampFromString(f.Value)
	return err
}

// --info.description
var infoDescriptionFlag = flag.NewStringFlag("info.description", "provide a description for the app", hideFlags, infoDescriptionFlagValidator)
var infoDescriptionFlagValidator = func(f flag.StringFlag) error {
	overrideVCFG.Info.Description = f.Value
	return nil
}

// --info.name
var infoNameFlag = flag.NewStringFlag("info.name", "name of the app", hideFlags, infoNameFlagValidator)
var infoNameFlagValidator = func(f flag.StringFlag) error {
	overrideVCFG.Info.Name = f.Value
	return nil
}

// --info.summary
var infoSummaryFlag = flag.NewStringFlag("info.summary", "provide a short summary for the app", hideFlags, infoSummaryFlagValidator)
var infoSummaryFlagValidator = func(f flag.StringFlag) error {
	overrideVCFG.Info.Summary = f.Value
	return nil
}

// --info.url
var infoURLFlag = flag.NewStringFlag("info.url", "URL for more information about the app", hideFlags, infoURLFlagValidator)
var infoURLFlagValidator = func(f flag.StringFlag) error {
	var err error
	overrideVCFG.Info.URL, err = vcfg.URLFromString(f.Value)
	return err
}

// --info.version
var infoVersionFlag = flag.NewStringFlag("info.version", "identify the app's version", hideFlags, infoVersionFlagValidator)
var infoVersionFlagValidator = func(f flag.StringFlag) error {
	overrideVCFG.Info.Version = f.Value
	return nil
}

// --logging.config
var loggingConfigFlag = flag.NewNStringSliceFlag("logging[<<N>>].config", "configure app's logging config", &maxLoggingFlags, hideFlags, loggingConfigFlagValidator)
var loggingConfigFlagValidator = func(f flag.NStringSliceFlag) error {
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
}

// --logging.type
var loggingTypeFlag = flag.NewNStringFlag("logging[<<N>>].type", "configure app's logging type", &maxLoggingFlags, hideFlags, loggingTypeFlagValidator)
var loggingTypeFlagValidator = func(f flag.NStringFlag) error {
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
}

var initRequiredNFS = func(f flag.NStringFlag, fn func(nfs *vcfg.NFSSettings, s string)) error {
	return initFromNStringFlag(f, func(i int, s string) {
		for len(overrideVCFG.NFS) < i+1 {
			overrideVCFG.NFS = append(overrideVCFG.NFS, vcfg.NFSSettings{})
		}
		fn(&overrideVCFG.NFS[i], s)
	})
}

// --nfs.mount
var nfsMountFlag = flag.NewNStringFlag("nfs[<<N>>].mount", "configure app's nfs mounts", &maxNFSFlags, hideFlags, nfsMountFlagValidator)
var nfsMountFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredNFS(f, func(nfs *vcfg.NFSSettings, s string) { nfs.MountPoint = s })
}

// --nfs.options
var nfsOptionsFlag = flag.NewNStringFlag("nfs[<<N>>].options", "configure app's nfs options", &maxNFSFlags, hideFlags, nfsOptionsFlagValidator)
var nfsOptionsFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredNFS(f, func(nfs *vcfg.NFSSettings, s string) { nfs.Arguments = s })
}

// --nfs.server
var nfsServerFlag = flag.NewNStringFlag("nfs[<<N>>].server", "configure app's nfs servers", &maxNFSFlags, hideFlags, nfsServerFlagValidator)
var nfsServerFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredNFS(f, func(nfs *vcfg.NFSSettings, s string) { nfs.Server = s })
}

func initRequiredNetworks(l, i int) {
	if l == 0 {
		return
	}
	for len(overrideVCFG.Networks) < i+1 {
		overrideVCFG.Networks = append(overrideVCFG.Networks, vcfg.NetworkInterface{IP: "dhcp"})
	}
}

var initRequiredNetworksFromString = func(f flag.NStringFlag, fn func(nic *vcfg.NetworkInterface, s string)) error {
	return initFromNStringFlag(f, func(i int, s string) {
		initRequiredNetworks(len(f.Value), i)
		fn(&overrideVCFG.Networks[i], s)
	})
}

// --network.tcpdump
var networkTCPDumpFlag = flag.NewNBoolFlag("network[<<N>>].tcpdump", "configure this network to run with tcpdump", &maxNetworkFlags, hideFlags, networkTCPDumpFlagValidator)
var networkTCPDumpFlagValidator = func(f flag.NBoolFlag) error {
	for i := 0; i < *f.Total; i++ {
		initRequiredNetworks(len(f.Value), i)
		val := f.Value[i]
		overrideVCFG.Networks[i].TCPDUMP = val
	}
	return nil
}

// --network.gateway
var networkGatewayFlag = flag.NewNStringFlag("network[<<N>>].gateway", "configure app's network gateway", &maxNetworkFlags, hideFlags, networkGatewayFlagValidator)
var networkGatewayFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredNetworksFromString(f, func(nic *vcfg.NetworkInterface, s string) { nic.Gateway = s })
}

var networkFlagValidator = func(f flag.NStringSliceFlag, fn func(nic *vcfg.NetworkInterface, s interface{})) error {
	for i := 0; i < *f.Total; i++ {
		// Skip empty values from unassigned flags
		if f.Value[i] == nil {
			continue
		}

		initRequiredNetworks(len(f.Value[i]), i)
		s := f.Value[i]
		nic := &overrideVCFG.Networks[i]
		fn(nic, s)
	}
	return nil
}

// --network.http
var networkHTTPFlag = flag.NewNStringSliceFlag("network[<<N>>].http", "expose http port", &maxNetworkFlags, hideFlags, networkHTTPFlagValidator)
var networkHTTPFlagValidator = func(f flag.NStringSliceFlag) error {
	return networkFlagValidator(f, func(nic *vcfg.NetworkInterface, s interface{}) { nic.HTTP = s.([]string) })
}

// --network.https
var networkHTTPSFlag = flag.NewNStringSliceFlag("network[<<N>>].https", "expose https port", &maxNetworkFlags, hideFlags, networkHTTPSFlagValidator)
var networkHTTPSFlagValidator = func(f flag.NStringSliceFlag) error {
	return networkFlagValidator(f, func(nic *vcfg.NetworkInterface, s interface{}) { nic.HTTPS = s.([]string) })
}

// --network.ip
var networkIPFlag = flag.NewNStringFlag("network[<<N>>].ip", "configure app's network IP address", &maxNetworkFlags, hideFlags, networkIPFlagValidator)
var networkIPFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredNetworksFromString(f, func(nic *vcfg.NetworkInterface, s string) { nic.IP = s })
}

// --network.mtu
var networkMTUFlag = flag.NewNStringFlag("network[<<N>>].mtu", "configure app's network interface MTU", &maxNetworkFlags, hideFlags, networkMTUFlagValidator)
var networkMTUFlagValidator = func(f flag.NStringFlag) error {
	for i := 0; i < *f.Total; i++ {
		initRequiredNetworks(len(f.Value), i)
		s := f.Value[i]
		if s == "" {
			continue
		}
		x, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		nic := &overrideVCFG.Networks[i]
		nic.MTU = uint(x)
	}
	return nil
}

// --network.mask
var networkMaskFlag = flag.NewNStringFlag("network[<<N>>].mask", "configure app's subnet mask", &maxNetworkFlags, hideFlags, networkMaskFlagValidator)
var networkMaskFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredNetworksFromString(f, func(nic *vcfg.NetworkInterface, s string) { nic.Mask = s })
}

// --network.tcp
var networkTCPFlag = flag.NewNStringSliceFlag("network[<<N>>].tcp", "expose tcp port", &maxNetworkFlags, hideFlags, networkTCPFlagValidator)
var networkTCPFlagValidator = func(f flag.NStringSliceFlag) error {
	return networkFlagValidator(f, func(nic *vcfg.NetworkInterface, s interface{}) { nic.TCP = s.([]string) })
}

// --network.udp
var networkUDPFlag = flag.NewNStringSliceFlag("network[<<N>>].udp", "expose udp port", &maxNetworkFlags, hideFlags, networkUDPFlagValidator)
var networkUDPFlagValidator = func(f flag.NStringSliceFlag) error {
	return networkFlagValidator(f, func(nic *vcfg.NetworkInterface, s interface{}) { nic.UDP = s.([]string) })
}

// --system.kernel-args
var systemKernelArgsFlag = flag.NewStringFlag("system.kernel-args", "", hideFlags, systemKernelArgsFlagValidator)
var systemKernelArgsFlagValidator = func(f flag.StringFlag) error {
	overrideVCFG.System.KernelArgs = f.Value
	return nil
}

// --system.dns
var systemDNSFlag = flag.NewStringSliceFlag("system.dns", "set the DNS server list for the system", hideFlags, systemDNSFlagValidator)
var systemDNSFlagValidator = func(f flag.StringSliceFlag) error {
	overrideVCFG.System.DNS = f.Value
	return nil
}

// --system.hostname
var systemHostnameFlag = flag.NewStringFlag("system.hostname", "set the hostname for the system", hideFlags, systemHostnameFlagValidator)
var systemHostnameFlagValidator = func(f flag.StringFlag) error {
	overrideVCFG.System.Hostname = f.Value
	return nil
}

// --system.filesystem
var systemFilesystemFlag = flag.NewStringFlag("system.filesystem", "set the filesystem format", hideFlags, systemFilesystemFlagValidator)
var systemFilesystemFlagValidator = func(f flag.StringFlag) error {
	overrideVCFG.System.Filesystem = vcfg.Filesystem(f.Value)
	return nil
}

// --system.max-fds
var systemMaxFDsFlag = flag.NewUintFlag("system.max-fds", "maximum file descriptors available to app", hideFlags, systemMaxFDsFlagValidator)
var systemMaxFDsFlagValidator = func(f flag.UintFlag) error {
	overrideVCFG.System.MaxFDs = f.Value
	return nil
}

// --system.output-mode
var systemOutputModeFlag = flag.NewStringFlag("system.output-mode", "specify vm output behaviour mode", hideFlags, systemOutputModeFlagValidator)
var systemOutputModeFlagValidator = func(f flag.StringFlag) error {
	overrideVCFG.System.StdoutMode = vcfg.StdoutModeFromString(f.Value)
	return nil
}

// --system.user
var systemUserFlag = flag.NewStringFlag("system.user", "name of the non-root user (default: vorteil)", hideFlags, systemUserFlagValidator)
var systemUserFlagValidator = func(f flag.StringFlag) error {
	overrideVCFG.System.User = f.Value
	return nil
}

// --system.terminate-wait
var systemTerminateWaitFlag = flag.NewUintFlag("system.terminate-wait", "how long to wait after sending signal on program termination", hideFlags, ssystemTerminateWaitFlagValidator)
var ssystemTerminateWaitFlagValidator = func(f flag.UintFlag) error {
	overrideVCFG.System.TerminateWait = f.Value
	return nil
}

// a key can have multiple destinations
// key = src, vals = dst
var filesMap = make(map[string][]string)

// --files
var filesFlag = flag.NewStringSliceFlag("files", "<src>[@<dst>]   add files from the host filesystem to an existing folder in the virtual machine filesystem (dst defaults to '/')", hideFlags, filesFlagValidator)
var filesFlagValidator = func(f flag.StringSliceFlag) error {
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
}

// --vm.cpus
var vmCPUsFlag = flag.NewUintFlag("vm.cpus", "number of cpus to allocate to app", hideFlags, vmCPUsFlagValidator)
var vmCPUsFlagValidator = func(f flag.UintFlag) error {
	overrideVCFG.VM.CPUs = f.Value
	return nil
}

func overwriteSizeFieldFromString(f flag.StringFlag, field *vcfg.Bytes) error {
	var err error
	*field, err = vcfg.ParseBytes(f.Value)
	if err != nil {
		return fmt.Errorf("--%s=%s: %v", f.Key, f.Value, err)
	}
	return nil
}

// --vm.disk-size
var vmDiskSizeFlag = flag.NewStringFlag("vm.disk-size", "disk image capacity to allocate to app", hideFlags, vmDiskSizeFlagValidator)
var vmDiskSizeFlagValidator = func(f flag.StringFlag) error {
	return overwriteSizeFieldFromString(f, &overrideVCFG.VM.DiskSize)
}

// --vm.inodes
var vmInodesFlag = flag.NewUintFlag("vm.inodes", "number of inodes to build on disk image", hideFlags, vmInodesFlagValidator)
var vmInodesFlagValidator = func(f flag.UintFlag) error {
	overrideVCFG.VM.Inodes = vcfg.InodesQuota(f.Value)
	return nil
}

// --vm.kernel
var vmKernelFlag = flag.NewStringFlag("vm.kernel", "kernel to build app on", hideFlags, vmKernelFlagValidator)
var vmKernelFlagValidator = func(f flag.StringFlag) error {
	if f.Value != "" {
		overrideVCFG.VM.Kernel = f.Value
	}
	return nil
}

// --vm.ram
var vmRAMFlag = flag.NewStringFlag("vm.ram", "memory to allocate to app", hideFlags, vmRAMFlagValidator)
var vmRAMFlagValidator = func(f flag.StringFlag) error {
	return overwriteSizeFieldFromString(f, &overrideVCFG.VM.RAM)
}

var initFromNStringFlag = func(f flag.NStringFlag, fn func(i int, s string)) error {
	for i := 0; i < *f.Total; i++ {
		s := f.Value[i]
		if s == "" {
			continue
		}
		fn(i, s)
	}
	return nil
}

var initRequiredProgramsFromString = func(f flag.NStringFlag, fn func(prog *vcfg.Program, s string)) error {
	return initFromNStringFlag(f, func(i int, s string) {
		for len(overrideVCFG.Programs) < i+1 {
			overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
		}
		fn(&overrideVCFG.Programs[i], s)
	})
}

var initRequiredProgramsFromBool = func(f flag.NBoolFlag, fn func(prog *vcfg.Program, s bool)) error {
	for i := 0; i < *f.Total; i++ {
		for len(overrideVCFG.Programs) < i+1 {
			overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
		}
		fn(&overrideVCFG.Programs[i], f.Value[i])
	}
	return nil
}

var initRequiredProgramsFromStringSlice = func(f flag.NStringSliceFlag, fn func(prog *vcfg.Program, s []string)) error {
	for i := 0; i < *f.Total; i++ {
		s := f.Value[i]
		if s == nil {
			continue
		}
		for len(overrideVCFG.Programs) < i+1 {
			overrideVCFG.Programs = append(overrideVCFG.Programs, vcfg.Program{})
		}
		fn(&overrideVCFG.Programs[i], s)
	}
	return nil
}

// --program.binary
var programBinaryFlag = flag.NewNStringFlag("program[<<N>>].binary", "configure a program binary", &maxProgramFlags, hideFlags, programBinaryFlagValidator)
var programBinaryFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredProgramsFromString(f, func(prog *vcfg.Program, s string) { prog.Binary = s })
}

// --program.privileges
var programPrivilegesFlag = flag.NewNStringFlag("program[<<N>>].privilege", "configure program privileges (root, superuser, user)", &maxProgramFlags, hideFlags, programPrivilegesFlagValidator)
var programPrivilegesFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredProgramsFromString(f, func(prog *vcfg.Program, s string) { prog.Privilege = vcfg.Privilege(s) })
}

// --program.env
var programEnvFlag = flag.NewNStringSliceFlag("program[<<N>>].env", "configure the environment variables of a program", &maxProgramFlags, hideFlags, programEnvFlagValidator)
var programEnvFlagValidator = func(f flag.NStringSliceFlag) error {
	return initRequiredProgramsFromStringSlice(f, func(prog *vcfg.Program, s []string) { prog.Env = s })
}

// --program.cwd
var programCWDFlag = flag.NewNStringFlag("program[<<N>>].cwd", "configure the working directory of a program", &maxProgramFlags, hideFlags, programCWDFlagValidator)
var programCWDFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredProgramsFromString(f, func(prog *vcfg.Program, s string) { prog.Cwd = s })
}

// --program.terminate
var programTerminateFlag = flag.NewNStringFlag("program[<<N>>].terminate", "configure the signal to send program on termination", &maxProgramFlags, hideFlags, programTerminateFlagValidator)
var programTerminateFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredProgramsFromString(f, func(prog *vcfg.Program, s string) { prog.Terminate = vcfg.TerminateSignal(s) })
}

// --program.logfiles
var programLogFilesFlag = flag.NewNStringSliceFlag("program[<<N>>].logfiles", "gure the logfiles of a program", &maxProgramFlags, hideFlags, programLogFilesFlagValidator)
var programLogFilesFlagValidator = func(f flag.NStringSliceFlag) error {
	return initRequiredProgramsFromStringSlice(f, func(prog *vcfg.Program, s []string) { prog.LogFiles = s })
}

// --program.bootstrap
var programBootstrapFlag = flag.NewNStringSliceFlag("program[<<N>>].bootstrap", "configure the bootstrap parameters of a program", &maxProgramFlags, hideFlags, programBootstrapFlagValidator)
var programBootstrapFlagValidator = func(f flag.NStringSliceFlag) error {
	return initRequiredProgramsFromStringSlice(f, func(prog *vcfg.Program, s []string) { prog.Bootstrap = s })
}

// --program.stdout
var programStdoutFlag = flag.NewNStringFlag("program[<<N>>].stdout", "configure programs stdout", &maxProgramFlags, hideFlags, programStdoutFlagValidator)
var programStdoutFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredProgramsFromString(f, func(prog *vcfg.Program, s string) { prog.Stdout = s })
}

// --program.stderr
var programStderrFlag = flag.NewNStringFlag("program[<<N>>].stderr", "configure programs stderr", &maxProgramFlags, hideFlags, programStderrFlagValidator)
var programStderrFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredProgramsFromString(f, func(prog *vcfg.Program, s string) { prog.Stderr = s })
}

// --program.strace
var programStraceFlag = flag.NewNBoolFlag("program[<<N>>].strace", "configure the program to run with strace", &maxProgramFlags, hideFlags, programStraceFlagValidator)
var programStraceFlagValidator = func(f flag.NBoolFlag) error {
	return initRequiredProgramsFromBool(f, func(prog *vcfg.Program, s bool) { prog.Strace = s })
}

// --program.args
var programArgsFlag = flag.NewNStringFlag("program[<<N>>].args", "configure programs args", &maxProgramFlags, hideFlags, programArgsFlagValidator)
var programArgsFlagValidator = func(f flag.NStringFlag) error {
	return initRequiredProgramsFromString(f, func(prog *vcfg.Program, s string) { prog.Args = s })
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
