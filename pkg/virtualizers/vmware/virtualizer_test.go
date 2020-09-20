package vmware

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vorteil/vorteil/pkg/vcfg"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
)

var codeBlockToLookIP = `
Windows Hypervisor Platform accelerator is operational
[    0.900000]  #vorteil-20.5.3-rc1 (4b58d09) SMP 20-05-2020 (Linux version 5.5.13+)
[    0.920000]  ip: 10.0.2.15
[    0.920000]  mask: 255.255.255.0
[    0.930000]  gateway: 10.0.2.2
[    0.930000]  dns: 10.0.2.3, 1.1.1.1, 1.0.0.1
2020/05/27 21:17:53 No background color set in BACKGROUND environment variable
2020/05/27 21:17:53 Binding port: 8888
`

var genVMX = `
#!/usr/bin/vmware
debugStub.listen.guest64 = "TRUE"
debugStub.hideBreakpoints= "TRUE"
debugStub.listen.guest64.remote = "TRUE"
.encoding = "UTF-8"
config.version = "8"
bios.bootdelay = "0"
virtualHW.version = "11"
vcpu.hotadd = "FALSE"
sata0.present = "FALSE"
memsize = "1000"
mem.hotadd = "FALSE"

scsi0.present = "TRUE"
scsi0.virtualDev = "pvscsi"
scsi0:0.present = "TRUE"
scsi0:0.fileName = "100"

ethernet0.present = "true"
ethernet0.connectionType = "nat"
ethernet0.virtualDev = "vmxnet3"
ethernet0.wakeOnPcktRcv = "FALSE"
ethernet0.addressType = "generated"
ethernet0.pciSlotNumber = "1024"
ethernet1.present = "false"
ethernet1.connectionType = "nat"
ethernet1.virtualDev = "vmxnet3"
ethernet1.wakeOnPcktRcv = "FALSE"
ethernet1.addressType = "generated"
ethernet1.pciSlotNumber = "1025"
ethernet2.present = "false"
ethernet2.connectionType = "nat"
ethernet2.virtualDev = "vmxnet3"
ethernet2.wakeOnPcktRcv = "FALSE"
ethernet2.addressType = "generated"
ethernet2.pciSlotNumber = "1026"
ethernet3.present = "false"
ethernet3.connectionType = "nat"
ethernet3.virtualDev = "vmxnet3"
ethernet3.wakeOnPcktRcv = "FALSE"
ethernet3.addressType = "generated"
ethernet3.pciSlotNumber = "1027"
usb.present = "FALSE"
mks.enable3d = "FALSE"
svga.graphicsMemoryKB = "786432"
pciBridge0.present = "TRUE"
pciBridge4.present = "TRUE"
pciBridge4.virtualDev = "pcieRootPort"
pciBridge4.functions = "8"
pciBridge5.present = "TRUE"
pciBridge5.virtualDev = "pcieRootPort"
pciBridge5.functions = "8"
pciBridge6.present = "TRUE"
pciBridge6.virtualDev = "pcieRootPort"
pciBridge6.functions = "8"
pciBridge7.present = "TRUE"
hpet0.present = "FALSE"
usb.vbluetooth.startConnected = "TRUE"
displayName = "helloworld"
guestOS = "Other"
nvram = "/tmp/test/helloworld.nvram"
virtualHW.productCompatibility = "hosted"
powerType.powerOff = "soft"
powerType.powerOn = "soft"
powerType.suspend = "soft"
powerType.reset = "soft"
replay.supported = "FALSE"
replay.filename = ""
sata0:0.redo = ""
pciBridge0.pciSlotNumber = "17"
pciBridge4.pciSlotNumber = "21"
pciBridge5.pciSlotNumber = "22"
pciBridge6.pciSlotNumber = "23"
pciBridge7.pciSlotNumber = "24"
scsi0.pciSlotNumber = "16"
usb.pciSlotNumber = "32"
vmci0.pciSlotNumber = "36"
sata0.pciSlotNumber = "37"
vmci0.id = "-1035185677"
monitor.phys_bits_used = "42"
vmotion.checkpointFBSize = "33554432"
vmotion.checkpointSVGAPrimarySize = "33554432"
cleanShutdown = "TRUE"
softPowerOff = "FALSE"
usb:0.present = "FALSE"
usb:0.deviceType = "hid"
usb:0.port = "0"
usb:0.parent = "-1"
usb:1.speed = "2"
usb:1.present = "FALSE"
usb:1.deviceType = "hub"
usb:1.port = "1"
usb:1.parent = "-1"
numvcpus = "1"
sata0:1.present = "FALSE"
ehci.present = "FALSE"
sound.present = "FALSE"
serial0.present = "TRUE"
serial0.fileType = "pipe"
serial0.fileName = "\\.\pipe\test"
floppy0.present = "FALSE"
extendedConfigFile = "/tmp/test/helloworld.vmxf"
log.fileName = "/tmp/test/helloworld.log"
msg.autoAnswer = "TRUE"
rtc.diffFromUTC = 0
`

func TestGenVMX(t *testing.T) {
	vmx := GenerateVMX("1", "1000", "100", "helloworld", "/tmp/test", 1, "nat", "test")
	if strings.TrimSpace(genVMX) != strings.TrimSpace(vmx) {
		t.Errorf("generating vmx failed does not match stored variable")
	}
}

func TestLogWrite(t *testing.T) {
	v := &Virtualizer{
		virtLogger: logger.NewLogger(2048),
	}
	exactText := []byte(fmt.Sprintf("%s%s%s\n", "\033[31m", "hello", "\033[0m"))
	v.log("error", "%s", "hello")

	sub := v.virtLogger.Subscribe()

	var logs []byte
	var done bool
	for !done {
		select {
		case logdata, more := <-sub.Inbox():
			if !more {
				break
			}
			logs = append(logs, logdata...)
		default:
			done = true
		}
	}

	if strings.TrimSpace(string(logs)) != strings.TrimSpace(string(exactText)) {
		t.Errorf("logging \"hello\" failed, expected \"%v\" but got \"%v\"", strings.TrimSpace(string(exactText)), strings.TrimSpace(string(logs)))
	}

}
func TestInitialize(t *testing.T) {
	var c = &Config{
		Headless:    true,
		NetworkType: "nat",
	}

	v := &Virtualizer{}
	data := c.Marshal()
	err := v.Initialize(data)
	if err != nil {
		t.Errorf("initialize failed, expected to be successful but ended up with an error %v", err)
	}

	if !v.headless {
		t.Errorf("initialize failed, expected headless to be %v but got %v", true, v.headless)
	}
	if v.networkType != "nat" {
		t.Errorf("initialize failed, expected networkType to be %s but got %s", "nat", v.networkType)
	}
}
func TestState(t *testing.T) {
	v := &Virtualizer{
		state: "ready",
	}
	state := v.State()
	if state != "ready" {
		t.Errorf("was unable to get state, expected %s but got %s", "ready", state)
	}
}
func TestType(t *testing.T) {
	v := &Virtualizer{}

	typeSt := v.Type()
	if typeSt != "vmware" {
		t.Errorf("expected %s but got %s", "vmware", typeSt)
	}
}
func TestLoggerAndSerial(t *testing.T) {
	v := &Virtualizer{
		virtLogger:   logger.NewLogger(2048),
		serialLogger: logger.NewLogger(2048),
	}

	virtl := v.Logs()
	seriall := v.Serial()

	if virtl == nil || seriall == nil {
		t.Errorf("unable to get loggers from virtualizer")
	}
}

func TestDownload(t *testing.T) {
	f, err := os.Create(filepath.Join(os.TempDir(), "disk.vmdk"))
	if err != nil {
		t.Errorf("unable to create temp file")
	}
	defer f.Close()
	v := &Virtualizer{
		virtLogger: logger.NewLogger(2048),
		disk:       f,
		state:      "ready",
	}

	file, err := v.Download()
	if err != nil {
		t.Errorf("unable to retrieve disk from virtualizer received error: %v", err)
	}
	if file == nil {
		t.Errorf("file retrieved was nil")
	}

}
func TestRoutes(t *testing.T) {
	httpArr := []string{"8888"}
	http := &vcfg.NetworkInterface{
		HTTP: httpArr,
		IP:   "dhcp",
	}
	vcfgI := []vcfg.NetworkInterface{*http}
	vcfg := &vcfg.VCFG{
		Networks: vcfgI,
	}
	v := &Virtualizer{
		virtLogger: logger.NewLogger(2048),
		config:     vcfg,
	}

	ni := v.Routes()
	for _, n := range ni {
		for _, typep := range n.HTTP {
			if typep.Port != "8888" {
				t.Errorf("fetching routes failed expected output %v but got %v", 8888, typep.Port)
			}
		}
	}
}
