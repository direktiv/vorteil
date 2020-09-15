package firecracker

import (
	"fmt"
	"strings"
	"testing"

	logger "github.com/vorteil/vorteil/pkg/virtualizers/logging"
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
func TestLookForIp(t *testing.T) {
	v := &Virtualizer{
		serialLogger: logger.NewLogger(2048),
	}

	v.serialLogger.Write([]byte(codeBlockToLookIP))

	address := v.lookForIP()
	if address != "10.0.2.15" {
		t.Errorf("unable to retrieve correct IP was expecting %s but got %s", "10.0.2.15", address)
	}
}
