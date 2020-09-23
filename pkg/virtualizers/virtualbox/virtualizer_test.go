package virtualbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vorteil/vorteil/pkg/vcfg"
	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
	"github.com/vorteil/vorteil/pkg/virtualizers/util"
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

func TestInitialize(t *testing.T) {
	var c = &Config{
		Headless:      true,
		NetworkType:   "nat",
		NetworkDevice: "",
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
	if typeSt != "virtualbox" {
		t.Errorf("expected %s but got %s", "virtualbox", typeSt)
	}
}

func TestLoggerAndSerial(t *testing.T) {
	v := &Virtualizer{
		serialLogger: logger.NewLogger(2048),
	}

	seriall := v.Serial()

	if seriall == nil {
		t.Errorf("unable to get loggers from virtualizer")
	}
}

func TestCreateVM(t *testing.T) {
	exactArgs := []string{"createvm", "--basefolder", "/tmp/test", "--name", "helloworld", "--register"}
	args := createVM("/tmp/test", "helloworld")
	if strings.Join(exactArgs, " ") != strings.Join(args, " ") {
		t.Errorf("create vm args failed, expected %s but got %s", exactArgs, args)
	}

}

// func TestModifyVM(t *testing.T) {
// 	exactArgs := []string{"modifyvm", "helloworld",
// 		"--memory", "1024", "--acpi", "on",
// 		"--ioapic", "on", "--cpus", "1",
// 		"--longmode", "on", "--largepages", "on", "--chipset", "ich9",
// 		"--bioslogofadein", "off", "--bioslogofadeout", "off",
// 		"--bioslogodisplaytime", "1", "--biosbootmenu", "disabled",
// 		"--rtcuseutc", "on", "--uart1", "0x3F8", "4", "--uartmode1",
// 		"server", "/tmp/test/socket"}

// 	argTest := modifyVM("helloworld", "1024", "1", "/tmp/test/socket")
// 	if strings.Join(exactArgs, " ") != strings.Join(argTest, " ") {
// 		t.Errorf("args do not match, expected %v but got %v", exactArgs, argTest)
// 	}
// }

func TestDownload(t *testing.T) {
	f, err := os.Create(filepath.Join(os.TempDir(), "disk.vmdk"))
	if err != nil {
		t.Errorf("unable to create temp file")
	}
	defer f.Close()
	v := &Virtualizer{
		disk:  f,
		state: "ready",
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

	ni := util.Routes(vcfg.Networks)
	for _, n := range ni {
		for _, typep := range n.HTTP {
			if typep.Port != "8888" {
				t.Errorf("fetching routes failed expected output %v but got %v", 8888, typep.Port)
			}
		}
	}
}
