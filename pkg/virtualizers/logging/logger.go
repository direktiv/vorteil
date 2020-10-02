package logger

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"io"
	"sync"

	"github.com/armon/circbuf"
)

const channelCapacity = 64

// Logger ..
type Logger struct {
	lock   sync.Mutex
	closed bool
	subs   map[*Subscription]bool
	buf    *circbuf.Buffer
}

// NewLogger ..
func NewLogger(bufferCapacity int64) *Logger {
	l := new(Logger)
	l.subs = make(map[*Subscription]bool)
	l.buf, _ = circbuf.NewBuffer(bufferCapacity)
	return l
}

// Close ..
func (l *Logger) Close() error {

	if l.closed {
		return nil
	}

	l.lock.Lock()
	defer l.lock.Unlock()

	if l.closed {
		return nil
	}

	for len(l.subs) > 0 {
		var sub *Subscription
		for k := range l.subs {
			sub = k
			break
		}
		sub.close()
	}

	l.closed = true
	return nil
}

// Write ..
func (l *Logger) Write(p []byte) (n int, err error) {

	if l.closed {
		err = io.EOF
		return
	}

	l.lock.Lock()
	defer l.lock.Unlock()

	if l.closed {
		err = io.EOF
		return
	}

	n, err = l.buf.Write(p)
	if err != nil {
		panic(err)
	}

	if n != len(p) {
		panic("n != len(p)")
	}

	buf := make([]byte, len(p))
	copy(buf, p)

	for s := range l.subs {
		select {
		case s.ch <- buf:
		default:
		}
	}

	return
}

// Subscribe ..
func (l *Logger) Subscribe() *Subscription {
	l.lock.Lock()
	defer l.lock.Unlock()

	s := new(Subscription)
	s.l = l
	s.ch = make(chan []byte, channelCapacity)
	x := l.buf.Bytes()
	buf := make([]byte, len(x))
	copy(buf, x)
	l.subs[s] = true

	s.ch <- buf

	if l.closed {
		close(s.ch)
	}

	return s
}

// Subscription ..
type Subscription struct {
	l  *Logger
	ch chan []byte
}

func (s *Subscription) close() {
	delete(s.l.subs, s)
	close(s.ch)
}

// Close ..
func (s *Subscription) Close() error {

	if s.l.closed {
		return nil
	}

	s.l.lock.Lock()
	defer s.l.lock.Unlock()

	if s.l.closed {
		return nil
	}

	s.close()
	for {
		_, more := <-s.ch
		if !more {
			break
		}
	}
	return nil
}

// Inbox ..
func (s *Subscription) Inbox() <-chan []byte {
	return s.ch
}
