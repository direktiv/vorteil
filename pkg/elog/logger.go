package elog

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/vbauerster/mpb/v5"
	"github.com/vbauerster/mpb/v5/decor"
)

type Logger interface {
	Debugf(format string, x ...interface{})
	Errorf(format string, x ...interface{})
	Infof(format string, x ...interface{})
	Printf(format string, x ...interface{})
	Warnf(format string, x ...interface{})
	IsInfoEnabled() bool
	IsDebugEnabled() bool
}

type Progress interface {
	Finish(success bool)
	Increment(n int64)
	Write(p []byte) (n int, err error)
	Seek(offset int64, whence int) (int64, error)
	ProxyReader(r io.Reader) io.ReadCloser
}

type ProgressReporter interface {
	NewProgress(label string, units string, total int64) Progress
}

type View interface {
	Logger
	ProgressReporter
}

type CLI struct {
	IsDebug            bool
	IsVerbose          bool
	lock               sync.Mutex
	isTrackingProgress bool
	bars               map[*mpb.Bar]bool
	buffer             *bytes.Buffer
	progressContainer  *mpb.Progress
}

func (log *CLI) Debugf(format string, x ...interface{}) {
	if log.IsDebug {
		logrus.Debugf(format, x...)
	}
}

func (log *CLI) Errorf(format string, x ...interface{}) {
	logrus.Errorf(format, x...)
}

func (log *CLI) Infof(format string, x ...interface{}) {
	if log.IsVerbose {
		logrus.Infof(format, x...)
	}
}

func (log *CLI) Printf(format string, x ...interface{}) {
	logrus.Printf(format, x...)
}

func (log *CLI) Warnf(format string, x ...interface{}) {
	logrus.Warnf(format, x...)
}

func (log *CLI) IsInfoEnabled() bool {
	return logrus.IsLevelEnabled(logrus.InfoLevel)
}

func (log *CLI) IsDebugEnabled() bool {
	return logrus.IsLevelEnabled(logrus.DebugLevel)
}

func (log *CLI) NewProgress(label string, units string, total int64) Progress {

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

	return &pb{
		log:   log,
		p:     p,
		total: total,
	}

}

type pb struct {
	log    *CLI
	p      *mpb.Bar
	closed bool
	total  int64
	cursor int64
	bar    int64
}

func (pb *pb) Increment(n int64) {
	pb.p.IncrInt64(n)
	pb.bar += n
}

func (pb *pb) Finish(success bool) {
	if pb.closed {
		return
	}
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

func (pb *pb) Write(p []byte) (n int, err error) {
	n = len(p)
	pb.cursor += int64(n)
	if pb.bar < pb.cursor {
		pb.Increment(pb.cursor - pb.bar)
	}
	return
}

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

func (pb *pb) ProxyReader(r io.Reader) io.ReadCloser {
	return pb.p.ProxyReader(r)
}

type mws struct {
	w []io.WriteSeeker
}

func MultiWriteSeeker(writeseekers ...io.WriteSeeker) io.WriteSeeker {
	return &mws{
		w: writeseekers,
	}
}

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
