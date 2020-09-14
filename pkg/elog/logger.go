package elog

import (
	"encoding/json"

	"github.com/cirruslabs/echelon"
)

type LogLevel uint32

const (
	ErrorLevel LogLevel = LogLevel(echelon.ErrorLevel)
	WarnLevel  LogLevel = LogLevel(echelon.WarnLevel)
	InfoLevel  LogLevel = LogLevel(echelon.InfoLevel)
	DebugLevel LogLevel = LogLevel(echelon.DebugLevel)
	TraceLevel LogLevel = LogLevel(echelon.TraceLevel)
)

type Logger interface {
	Debugf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Finish(success bool)
	Infof(format string, args ...interface{})
	IsLogLevelEnabled(level LogLevel) bool
	Logf(level LogLevel, format string, args ...interface{})
	Scoped(scope string) Logger
	Tracef(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	SetStatus(key string, val interface{})
}

type EchelonLogger struct {
	*echelon.Logger
	status   string
	finished bool
}

func NewEchelonLogger() *EchelonLogger {
	return &EchelonLogger{
		status: "{}",
	}
}

func (elog *EchelonLogger) IsLogLevelEnabled(level LogLevel) bool {
	return elog.Logger.IsLogLevelEnabled(echelon.LogLevel(level))
}

func (elog *EchelonLogger) Logf(level LogLevel, format string, args ...interface{}) {
	elog.Logger.Logf(echelon.LogLevel(level), format, args...)
}

func (elog *EchelonLogger) Scoped(scope string) Logger {
	return &EchelonLogger{
		Logger: elog.Logger.Scoped(scope),
	}
}

func (elog *EchelonLogger) Finish(success bool) {
	if elog.finished {
		return
	}
	elog.finished = true
	elog.Logger.Finish(success)
}

func (elog *EchelonLogger) SetStatus(key string, val interface{}) {

	m := make(map[string]interface{})
	err := json.Unmarshal([]byte(elog.status), &m)
	if err != nil {
		panic(err)
	}

	m[key] = val
	data, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}

	elog.status = string(data)

}
