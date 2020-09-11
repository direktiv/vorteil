package logger

import (
	"testing"
)

// TestLogging creates a logger writes a line and check if it matches
func TestLogging(t *testing.T) {
	logWriter := NewLogger(2048)
	logWriter.Write([]byte("hello"))

	sub := logWriter.Subscribe()

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

	if string(logs) != "hello" {
		t.Errorf("logging \"hello\" failed, expected \"%v\" but got \"%v\"", "hello", string(logs))
	}
}
