package firecracker

import (
	"testing"
)

var codeBlockToLookIP = `
[1.770000] #vorteil-0.0.0 (191727a) SMP 14-06-2020 (Linux version 5.5.13+)
[1.800000] eth0 ip     : 174.72.0.23
[1.800000] eth0 mask   : 255.255.255.0
[1.810000] eth0 gateway: 174.72.0.1
[1.810000] dns: 174.72.0.1, 1.1.1.1, 1.0.0.1
[1.820000] VALUES CLIUD UNKNOWN UNKNOWN
2020/06/22 03:13:46 No background color set in BACKGROUND environment variable
2020/06/22 03:13:46 Binding port: 8888
 `

func TestInitialize(t *testing.T) {
	var c = &Config{}

	v := &Virtualizer{}
	data := c.Marshal()
	err := v.Initialize(data)
	if err != nil {
		t.Errorf("initialize failed, expected to be successful but ended up with an error %v", err)
	}

}

func TestType(t *testing.T) {
	v := &Virtualizer{}

	typeSt := v.Type()
	if typeSt != "firecracker" {
		t.Errorf("expected %s but got %s", "firecracker", typeSt)
	}
}

	seriall := v.Serial()

	if seriall == nil {
		t.Errorf("unable to get loggers from virtualizer")
	}
}
