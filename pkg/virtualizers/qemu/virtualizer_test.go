package qemu

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vorteil/vorteil/pkg/vcfg"

	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
)

func TestLoggerAndSerial(t *testing.T) {
	v := &Virtualizer{
		serialLogger: logger.NewLogger(2048),
	}

	seriall := v.Serial()

	if seriall == nil {
		t.Errorf("unable to get loggers from virtualizer")
	}
}
func TestInitialize(t *testing.T) {
	var c = &Config{
		Headless: true,
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
	if typeSt != "qemu" {
		t.Errorf("expected %s but got %s", "qemu", typeSt)
	}
}

func TestNetworkArgs(t *testing.T) {
	httpArr := []string{"8888"}
	exactNetArgs := []string{"-netdev", "user,id=network0,hostfwd=tcp::-:8888", "-device", "virtio-net-pci,netdev=network0,id=virtio0,mac=26:10:05:00:00:0a"}
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
	v.routes = ni

	args, err := v.initializeNetworkCards()
	if err != nil {
		t.Errorf("unable to initialize network cards ran into error: %v", err)
	}

	for i, arg := range args {
		if exactNetArgs[i] != arg {
			t.Errorf("unable read network args properly returned %v but was expecting %v", args, exactNetArgs)
		}
	}
}

func TestCreateArgs(t *testing.T) {
	// args := createArgs(uint(1), int(1000), true, filepath.Join(os.TempDir(), "disk.raw"), "raw")
	// expectedArgs := fmt.Sprintf("%s -no-reboot -machine q35 -smp 1 -m 1000 -serial stdio -display none -device virtio-scsi-pci,id=scsi -device scsi-hd,drive=hd0 -drive if=none,file=\"C:\\Users\\trent\\AppData\\Local\\Temp\\disk.raw\",format=raw,id=hd0", osFlags)
	//
	// if args != expectedArgs {
	// 	t.Errorf("createArgs failed, expected %s but got %s", expectedArgs, args)
	// }
}

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
	v := &Virtualizer{
		config: vcfg,
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
