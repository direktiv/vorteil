package elog

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"github.com/vorteil/vorteil/pkg/vio"

	"github.com/fatih/color"
	"github.com/sirupsen/logrus"
	"github.com/vbauerster/mpb/v5"
	"github.com/vbauerster/mpb/v5/decor"
)

// Logger is an interface that has the ability to hide debug/info
type Logger interface {
	Debugf(format string, x ...interface{})
	Errorf(format string, x ...interface{})
	Infof(format string, x ...interface{})
	Printf(format string, x ...interface{})
	Warnf(format string, x ...interface{})
	IsInfoEnabled() bool
	IsDebugEnabled() bool
}

// Progress is an interface to display progress bars for certain operations
type Progress interface {
	Finish(success bool)
	Increment(n int64)
	Write(p []byte) (n int, err error)
	Seek(offset int64, whence int) (int64, error)
	ProxyReader(r io.Reader) io.ReadCloser
}

// ProgressReporter is an interface that contains the ability to create a Progress bar object.
type ProgressReporter interface {
	NewProgress(label string, units string, total int64) Progress
}

// View is an interface that contains a logger and the ability to create progress objects
type View interface {
	Logger
	ProgressReporter
}

// CLI is a generic object setup for logging to terminal outputs
type CLI struct {
	DisableColors      bool
	DisableTTY         bool
	IsDebug            bool
	IsVerbose          bool
	lock               sync.Mutex
	isTrackingProgress bool
	bars               map[*mpb.Bar]bool
	buffer             *bytes.Buffer
	progressContainer  *mpb.Progress
}

// Debugf is a wrapper function that executes logrus.Tracef if debug is enabled.
func (log *CLI) Debugf(format string, x ...interface{}) {
	if log.IsDebug {
		logrus.Tracef(format, x...)
	}
}

// Errorf is a wrapper function that executes logrus.Errorf
func (log *CLI) Errorf(format string, x ...interface{}) {
	logrus.Errorf(format, x...)
}

// Infof is a wrapper function that executes logrus.Debugf only if verbose is enabled.
func (log *CLI) Infof(format string, x ...interface{}) {
	if log.IsVerbose {
		logrus.Debugf(format, x...)
	}
}

// Printf is a wrapper function that executes logrus.Printf
func (log *CLI) Printf(format string, x ...interface{}) {
	logrus.Printf(format, x...)
}

// Warnf is a wrapper function that executes logrus.Warnf
func (log *CLI) Warnf(format string, x ...interface{}) {
	logrus.Warnf(format, x...)
}

// IsInfoEnabled returns whether InfoLevel logging is enabled
func (log *CLI) IsInfoEnabled() bool {
	return logrus.IsLevelEnabled(logrus.InfoLevel)
}

// IsDebugEnabled returns whether DebugLevel logging is enabled
func (log *CLI) IsDebugEnabled() bool {
	return logrus.IsLevelEnabled(logrus.DebugLevel)
}

// NewProgress creates a progress object and returns
func (log *CLI) NewProgress(label string, units string, total int64) Progress {

	if log.DisableTTY {
		return &nilProgress{
			total: total,
		}
	}

	log.lock.Lock()
	defer log.lock.Unlock()

	if !log.isTrackingProgress {
		log.isTrackingProgress = true
		log.buffer = new(bytes.Buffer)
		logrus.SetOutput(log.buffer)
		log.progressContainer = mpb.New(mpb.WithWidth(80))
		log.bars = make(map[*mpb.Bar]bool)
	}

	var decorators []decor.Decorator
	switch units {
	default:
		fallthrough
	case "%":
		decorators = append(decorators, decor.Percentage())
	case "KiB":
		decorators = append(decorators, decor.Counters(decor.UnitKiB, "% .1f / % .1f"))
	}

	var p *mpb.Bar
	if total == 0 {
		p = log.progressContainer.AddSpinner(0, mpb.SpinnerOnLeft,
			mpb.PrependDecorators(
				decor.Name(label, decor.WC{W: len(label) + 1, C: decor.DidentRight}),
			),
		)
	} else {
		p = log.progressContainer.AddBar(total,
			// mpb.BarStyle("╢▌▌░╟"),
			mpb.PrependDecorators(
				// display our name with one space on the right
				decor.Name(label, decor.WC{W: len(label) + 1, C: decor.DidentRight}),
				// replace ETA decorator with "done" message, OnComplete event
				decor.OnComplete(
					decor.AverageETA(decor.ET_STYLE_GO, decor.WC{W: 4}), "done",
				),
			),
			mpb.AppendDecorators(decorators...),
		)
	}

	log.bars[p] = true

	pb := &pb{
		log:      log,
		p:        p,
		total:    total,
		interval: time.Millisecond * 100,
	}
	pb.nextUpdate = time.Now().Add(pb.interval)

	return pb

}

type nilProgress struct {
	cursor int64
	total  int64
}

// Increment nilProgress does nothing...
func (np *nilProgress) Increment(n int64) {

}

// Finish nilProgress does nothing...
func (np *nilProgress) Finish(success bool) {

}

// Write nilProgress writes to the cursor
func (np *nilProgress) Write(p []byte) (n int, err error) {
	n = len(p)
	np.cursor += int64(n)
	return
}

// Seek nilProgress creates the infinite loader symbol
func (np *nilProgress) Seek(offset int64, whence int) (int64, error) {
	var abs int64

	switch whence {
	case io.SeekCurrent:
		abs = np.cursor + offset
	case io.SeekStart:
		abs = offset
	case io.SeekEnd:
		abs = np.total + offset
	default:
		return 0, errors.New("invalid whence")
	}

	np.cursor = abs
	return abs, nil
}

// ProxyReader nillProgress does nothing with the reader
func (np *nilProgress) ProxyReader(r io.Reader) io.ReadCloser {

	if rc, ok := r.(io.ReadCloser); ok {
		return rc
	}

	return ioutil.NopCloser(r)
}

type pb struct {
	log    *CLI
	p      *mpb.Bar
	closed bool
	total  int64
	cursor int64
	bar    int64

	buffered   int64
	interval   time.Duration
	nextUpdate time.Time
}

// Increment increaes the progress on the bar
func (pb *pb) Increment(n int64) {
	pb.buffered += n
	pb.bar += n
	if !time.Now().Before(pb.nextUpdate) {
		pb.flush()
	}
}

func (pb *pb) flush() {
	pb.nextUpdate = time.Now().Add(pb.interval)
	pb.p.IncrInt64(pb.buffered)
	pb.buffered = 0
}

// Finish closes the progress bar object
func (pb *pb) Finish(success bool) {
	if pb.closed {
		return
	}
	pb.flush()
	pb.closed = true
	if pb.bar != pb.total || pb.total == 0 || !success {
		pb.p.Abort(false)
	}

	pb.log.lock.Lock()
	defer pb.log.lock.Unlock()
	delete(pb.log.bars, pb.p)

	if len(pb.log.bars) == 0 {
		pb.log.bars = nil
		pb.log.isTrackingProgress = false
		pb.log.progressContainer.Wait()
		pb.log.progressContainer = nil
		logrus.SetOutput(os.Stdout)
		_, _ = pb.log.buffer.WriteTo(os.Stdout)
		pb.log.buffer = nil
	}
}

// Write writes to the progress bar object
func (pb *pb) Write(p []byte) (n int, err error) {
	n = len(p)
	pb.cursor += int64(n)
	if pb.bar < pb.cursor {
		pb.Increment(pb.cursor - pb.bar)
	}
	return
}

// Seek applies the offset to the progressbar cursor
func (pb *pb) Seek(offset int64, whence int) (int64, error) {
	var abs int64

	switch whence {
	case io.SeekCurrent:
		abs = pb.cursor + offset
	case io.SeekStart:
		abs = offset
	case io.SeekEnd:
		abs = pb.total + offset
	default:
		return 0, errors.New("invalid whence")
	}

	pb.cursor = abs
	if pb.bar < pb.cursor {
		pb.Increment(pb.cursor - pb.bar)
	}

	return abs, nil
}

// ProxyReader returns the ready of the progress bar
func (pb *pb) ProxyReader(r io.Reader) io.ReadCloser {

	pr := pb.p.ProxyReader(r)

	return vio.LazyReadCloser(
		func() (io.Reader, error) {
			return pr, nil
		},
		func() error {
			pb.flush()
			pb.Finish(pb.total == pb.bar)
			return pr.Close()
		},
	)

}

type mws struct {
	w []io.WriteSeeker
}

// MultiWriteSeeker returns a MultiWriteSeeker object
func MultiWriteSeeker(writeseekers ...io.WriteSeeker) io.WriteSeeker {
	return &mws{
		w: writeseekers,
	}
}

// Write writes to the multi write seekers
func (mws *mws) Write(p []byte) (n int, err error) {
	for _, w := range mws.w {
		n, err = w.Write(p)
		if err != nil {
			return
		}
		if n != len(p) {
			err = io.ErrShortWrite
			return
		}
	}
	return len(p), nil
}

// Seek moves to the offset
func (mws *mws) Seek(offset int64, whence int) (int64, error) {

	var abs int64
	abs, err := mws.w[0].Seek(offset, whence)
	if err != nil {
		return 0, err
	}

	for _, w := range mws.w[1:] {
		n, err := w.Seek(offset, whence)
		if err != nil {
			return 0, err
		}
		if n != abs {
			err = io.ErrShortWrite
			return 0, err
		}
	}
	return abs, nil
}

// Format formats our logger for terminal use
func (log *CLI) Format(entry *logrus.Entry) ([]byte, error) {

	faint := color.New(color.Faint).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	blue := color.New(color.FgBlue).SprintFunc()

	x := entry.Message
	if !log.DisableColors {
		switch entry.Level {
		case logrus.TraceLevel:
			x = fmt.Sprintf("%s\n", faint(x))
		case logrus.DebugLevel:
			x = fmt.Sprintf("%s\n", blue(x))
		case logrus.InfoLevel:
			x = fmt.Sprintf("%s\n", x)
		case logrus.WarnLevel:
			x = fmt.Sprintf("%s\n", yellow(x))
		case logrus.ErrorLevel:
			x = fmt.Sprintf("%s\n", red(x))
		default:
		}
	}

	return []byte(x), nil

}
