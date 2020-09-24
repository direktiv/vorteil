package main

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vorteil/vorteil/pkg/vcfg"
)

func testResetOverrideVCFG() {
	overrideVCFG = vcfg.VCFG{}
}

func TestVMCPUsFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --vm.cpus=1
	f := vmCPUsFlag
	f.Value = 1

	err := vmCPUsFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, uint(1), overrideVCFG.VM.CPUs)

}

func TestVMDiskSizeFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --vm.disk-size="10 MiB"
	f := vmDiskSizeFlag
	f.Value = "10 MiB"
	nBytes := 10 * 1024 * 1024

	err := vmDiskSizeFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, vcfg.Bytes(nBytes), overrideVCFG.VM.DiskSize)

}

func TestVMInodesFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --vm.inodes=1337
	f := vmInodesFlag
	f.Value = 1337

	err := vmInodesFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, vcfg.InodesQuota(f.Value), overrideVCFG.VM.Inodes)

}

func TestVMKernelFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --vm.kernel="20.9.2"
	f := vmKernelFlag
	f.Value = "20.9.2"

	err := vmKernelFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, f.Value, overrideVCFG.VM.Kernel)

}

func TestVMRAMFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --vm.ram="128 MiB"
	f := vmRAMFlag
	f.Value = "128 MiB"
	nBytes := 128 * 1024 * 1024

	err := vmRAMFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, vcfg.Bytes(nBytes), overrideVCFG.VM.RAM)

}

func TestFilesFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --files="a@b" --files="a@c" --files="c@d"
	f := filesFlag
	f.Value = []string{"a@b", "a@c", "c@d"}

	err := filesFlagValidator(f)
	assert.NoError(t, err)

	a, _ := filesMap["a"]
	c, _ := filesMap["c"]

	assert.Equal(t, a, []string{"b", "c"})
	assert.Equal(t, c, []string{"d"})

}

func TestInfoAuthorFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --info.author="John Connor"
	f := infoAuthorFlag
	f.Value = "John Connor"

	err := infoAuthorFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, f.Value, overrideVCFG.Info.Author)

}

func TestInfoDateFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --info.date="29-08-1997"
	f := infoDateFlag
	f.Value = "29-08-1997"

	err := infoDateFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, f.Value, overrideVCFG.Info.Date.String())

}

func TestInfoDescriptionFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --info.description="Hasta la vista, baby."
	f := infoDescriptionFlag
	f.Value = "Hasta la vista, baby."

	err := infoDescriptionFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, f.Value, overrideVCFG.Info.Description)

}

func TestInfoNameFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --info.name="Terminator 2"
	f := infoNameFlag
	f.Value = "Terminator 2"

	err := infoNameFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, f.Value, overrideVCFG.Info.Name)

}

func TestInfoSummaryFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --info.summary="Judgment Day"
	f := infoSummaryFlag
	f.Value = "Judgment Day"

	err := infoSummaryFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, f.Value, overrideVCFG.Info.Summary)

}

func TestInfoURLFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --info.url="https://en.wikipedia.org/wiki/Terminator_2:_Judgment_Day"
	f := infoURLFlag
	f.Value = "https://en.wikipedia.org/wiki/Terminator_2:_Judgment_Day"

	err := infoURLFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, vcfg.URL(f.Value), overrideVCFG.Info.URL)

}

func TestInfoVersionFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --info.version="T101"
	f := infoVersionFlag
	f.Value = "T101"

	err := infoVersionFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, f.Value, overrideVCFG.Info.Version)

}

func TestNetworksIPFlag(t *testing.T) {

	testResetOverrideVCFG()
	var ip = "29.08.19.97"

	// set --networks[3].ip="29.08.19.97"
	f := networkIPFlag
	nNICs := 3
	f.Total = &nNICs
	f.Value = []string{"", "", "29.08.19.97"}

	err := networkIPFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNICs, len(overrideVCFG.Networks))
	assert.Equal(t, ip, overrideVCFG.Networks[2].IP)
	assert.Equal(t, "dhcp", overrideVCFG.Networks[1].IP)
	assert.Equal(t, "dhcp", overrideVCFG.Networks[0].IP)

}

func TestNetworkMaskFlag(t *testing.T) {

	testResetOverrideVCFG()
	mask := "255.255.255.0"

	// set --networks[2].mask="255.255.255.0"
	f := networkMaskFlag
	nNICs := 2
	f.Total = &nNICs
	f.Value = []string{"", mask}

	err := networkMaskFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNICs, len(overrideVCFG.Networks))
	assert.Equal(t, mask, overrideVCFG.Networks[1].Mask)
	assert.Equal(t, "", overrideVCFG.Networks[0].Mask)

}

func TestNetworkGatewayFlag(t *testing.T) {

	testResetOverrideVCFG()
	gateway := "192.168.0.1"

	// set --networks[4].gateway="192.168.0.1"
	f := networkMaskFlag
	nNICs := 4
	f.Total = &nNICs
	f.Value = []string{"", "", "", gateway}

	err := networkGatewayFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNICs, len(overrideVCFG.Networks))
	assert.Equal(t, gateway, overrideVCFG.Networks[3].Gateway)
	assert.Equal(t, "", overrideVCFG.Networks[2].Gateway)
	assert.Equal(t, "", overrideVCFG.Networks[1].Gateway)
	assert.Equal(t, "", overrideVCFG.Networks[0].Gateway)

}

func TestNetworkUDPFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --networks[1].udp="80" --networks[2].udp="81" --networks[3].udp="82"
	f := networkUDPFlag
	nNICs := 3
	f.Total = &nNICs
	f.Value = [][]string{[]string{"80"}, []string{"81"}, []string{"82"}}

	err := networkUDPFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNICs, len(overrideVCFG.Networks))
	assert.Equal(t, f.Value[2], overrideVCFG.Networks[2].UDP)
	assert.Equal(t, f.Value[1], overrideVCFG.Networks[1].UDP)
	assert.Equal(t, f.Value[0], overrideVCFG.Networks[0].UDP)

}

func TestNetworkTCPFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --networks[1].tcp="80" --networks[2].tcp="81" --networks[3].tcp="82"
	f := networkTCPFlag
	nNICs := 3
	f.Total = &nNICs
	f.Value = [][]string{[]string{"80"}, []string{"81"}, []string{"82"}}

	err := networkTCPFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNICs, len(overrideVCFG.Networks))
	assert.Equal(t, f.Value[2], overrideVCFG.Networks[2].TCP)
	assert.Equal(t, f.Value[1], overrideVCFG.Networks[1].TCP)
	assert.Equal(t, f.Value[0], overrideVCFG.Networks[0].TCP)

}

func TestNetworkHTTPFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --networks[1].http="80" --networks[2].http="81" --networks[3].http="82"
	f := networkHTTPFlag
	nNICs := 3
	f.Total = &nNICs
	f.Value = [][]string{[]string{"80"}, []string{"81"}, []string{"82"}}

	err := networkHTTPFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNICs, len(overrideVCFG.Networks))
	assert.Equal(t, f.Value[2], overrideVCFG.Networks[2].HTTP)
	assert.Equal(t, f.Value[1], overrideVCFG.Networks[1].HTTP)
	assert.Equal(t, f.Value[0], overrideVCFG.Networks[0].HTTP)

}

func TestNetworkHTTPSFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --networks[1].https="80" --networks[2].https="81" --networks[3].https="82"
	f := networkHTTPSFlag
	nNICs := 3
	f.Total = &nNICs
	f.Value = [][]string{[]string{"80"}, []string{"81"}, []string{"82"}}

	err := networkHTTPSFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNICs, len(overrideVCFG.Networks))
	assert.Equal(t, f.Value[2], overrideVCFG.Networks[2].HTTPS)
	assert.Equal(t, f.Value[1], overrideVCFG.Networks[1].HTTPS)
	assert.Equal(t, f.Value[0], overrideVCFG.Networks[0].HTTPS)

}

func TestNetworkMTU(t *testing.T) {

	testResetOverrideVCFG()

	// set --networks[2].mtu=64
	f := networkMTUFlag
	nNICs := 2
	f.Total = &nNICs
	f.Value = []string{"", "64"}

	mtu, err := strconv.Atoi(f.Value[1])
	assert.NoError(t, err)

	err = networkMTUFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNICs, len(overrideVCFG.Networks))
	assert.Equal(t, uint(mtu), overrideVCFG.Networks[1].MTU)
	assert.Equal(t, uint(0), overrideVCFG.Networks[0].MTU)

}

func TestNetworkTCPDumpFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --networks[1].tcpdump=true
	f := networkTCPDumpFlag
	f.Value = []bool{true}
	nNICs := 1
	f.Total = &nNICs

	err := networkTCPDumpFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNICs, len(overrideVCFG.Networks))
	assert.Equal(t, f.Value[0], overrideVCFG.Networks[0].TCPDUMP)

}

func TestLoggingConfigFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --logging[1].config="test"
	f := loggingConfigFlag
	f.Value = [][]string{[]string{"test"}}
	nLog := 1
	f.Total = &nLog

	err := loggingConfigFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nLog, len(overrideVCFG.Logging))
	assert.Equal(t, f.Value[0], overrideVCFG.Logging[0].Config)

}

func TestLoggingTypeFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --logging[1].type="test"
	f := loggingTypeFlag
	f.Value = []string{"", "", "test"}
	nLog := 3
	f.Total = &nLog

	err := loggingTypeFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nLog, len(overrideVCFG.Logging))
	assert.Equal(t, f.Value[2], overrideVCFG.Logging[2].Type)

}

func TestNFSMountFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --nfs[2].mount="test"
	f := nfsMountFlag
	f.Value = []string{"test1", "test2"}
	nNFS := 2
	f.Total = &nNFS

	err := nfsMountFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNFS, len(overrideVCFG.NFS))
	assert.Equal(t, f.Value[1], overrideVCFG.NFS[1].MountPoint)
	assert.Equal(t, f.Value[0], overrideVCFG.NFS[0].MountPoint)

}

func TestNFSServerFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --nfs[2].server="test"
	f := nfsServerFlag
	f.Value = []string{"test1", "test2"}
	nNFS := 2
	f.Total = &nNFS

	err := nfsServerFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNFS, len(overrideVCFG.NFS))
	assert.Equal(t, f.Value[1], overrideVCFG.NFS[1].Server)
	assert.Equal(t, f.Value[0], overrideVCFG.NFS[0].Server)

}

func TestNFSOptionsFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --nfs[2].options="test"
	f := nfsOptionsFlag
	f.Value = []string{"test1", "test2"}
	nNFS := 2
	f.Total = &nNFS

	err := nfsOptionsFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, nNFS, len(overrideVCFG.NFS))
	assert.Equal(t, f.Value[1], overrideVCFG.NFS[1].Arguments)
	assert.Equal(t, f.Value[0], overrideVCFG.NFS[0].Arguments)

}

func TestSystemKernelArgsFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --system.kernel-args="loglevel=9"
	f := systemKernelArgsFlag
	f.Value = "loglevel=9"

	err := systemKernelArgsFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, f.Value, overrideVCFG.System.KernelArgs)

}

func TestSystemDNSFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --system.dns="1.1.1.1" --system.dns="8.8.8.8" --system.dns="8.8.4.4"
	f := systemDNSFlag
	f.Value = []string{"1.1.1.1", "8.8.8.8", "8.8.4.4"}

	err := systemDNSFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, len(f.Value), len(overrideVCFG.System.DNS))
	assert.Equal(t, f.Value[2], overrideVCFG.System.DNS[2])
	assert.Equal(t, f.Value[1], overrideVCFG.System.DNS[1])
	assert.Equal(t, f.Value[0], overrideVCFG.System.DNS[0])

}

func TestHostnameFlag(t *testing.T) {

	testResetOverrideVCFG()

	// set --system.hostname="skynet"
	f := systemHostnameFlag
	f.Value = "skynet"

	err := systemHostnameFlagValidator(f)
	assert.NoError(t, err)
	assert.Equal(t, f.Value, overrideVCFG.System.Hostname)

}
