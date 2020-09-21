package hyperv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/virtualizers"
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

func TestLoggerAndSerial(t *testing.T) {
	v := &Virtualizer{
		serialLogger: logger.NewLogger(2048),
	}

	seriall := v.Serial()

	if seriall == nil {
		t.Errorf("unable to get loggers from virtualizer")
	}
}

func TestType(t *testing.T) {
	v := &Virtualizer{}

	if v.Type() != "hyperv" {
		t.Errorf("typed returned from virtualizer is '%s' but expected '%s'", v.Type(), "hyperv")
	}
}

func TestState(t *testing.T) {
	v := &Virtualizer{
		state: "ready",
	}

	state := v.State()
	if state != "ready" {
		t.Errorf("unable to get state properly, expected 'ready' but got %s", state)
	}
}

func TestInitialize(t *testing.T) {
	var c = &Config{
		Headless:   true,
		SwitchName: "Default Switch",
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
	if v.switchName != "Default Switch" {
		t.Errorf("initialize failed, expected switchName to be %s but got %s", "Default Switch", v.switchName)
	}
}

func TestDownload(t *testing.T) {
	f, err := os.Create(filepath.Join(os.TempDir(), "disk.vhd"))
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
	v := &Virtualizer{
		config: vcfg,
	}

	ni := virtualizers.Routes(v)
	for _, n := range ni {
		for _, typep := range n.HTTP {
			if typep.Port != "8888" {
				t.Errorf("fetching routes failed expected output %v but got %v", 8888, typep.Port)
			}
		}
	}
}
