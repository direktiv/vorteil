package vmware

import (
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
		serialLogger: logger.NewLogger(2048),
	}

	seriall := v.Serial()

	if seriall == nil {
		t.Errorf("unable to get loggers from virtualizer")
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
