package elog

import (
	"bytes"
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
}

type ProgressReporter interface {
	NewProgress(label string, units string, total int64) Progress
}

type View interface {
	Logger
	ProgressReporter
}

type CLI struct {
	lock               sync.Mutex
	isTrackingProgress bool
	bars               map[*mpb.Bar]bool
	buffer             *bytes.Buffer
	progressContainer  *mpb.Progress
}

func (log *CLI) Debugf(format string, x ...interface{}) {
	logrus.Debugf(format, x...)
}

func (log *CLI) Errorf(format string, x ...interface{}) {
	logrus.Errorf(format, x...)
}

func (log *CLI) Infof(format string, x ...interface{}) {
	logrus.Infof(format, x...)
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

	var p *mpb.Bar
	if total == 0 {
		p = log.progressContainer.AddSpinner(0, mpb.SpinnerOnLeft,
			mpb.PrependDecorators(
				decor.Name(label, decor.WC{W: len(label) + 1, C: decor.DidentRight}),
			),
		)
	} else {
		p = log.progressContainer.AddBar(total,
			mpb.BarStyle("╢▌▌░╟"),
			mpb.PrependDecorators(
				// display our name with one space on the right
				decor.Name(label, decor.WC{W: len(label) + 1, C: decor.DidentRight}),
				// replace ETA decorator with "done" message, OnComplete event
				decor.OnComplete(
					decor.AverageETA(decor.ET_STYLE_GO, decor.WC{W: 4}), "done",
				),
			),
			mpb.AppendDecorators(decor.Percentage()),
		)
	}

	log.bars[p] = true

	return &pb{
		log: log,
		p:   p,
	}

}

type pb struct {
	log    *CLI
	p      *mpb.Bar
	closed bool
}

func (pb *pb) Increment(n int64) {
	pb.p.IncrInt64(n)
}

func (pb *pb) Finish(success bool) {
	if pb.closed {
		return
	}
	pb.closed = true
	pb.p.Abort(false)

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
