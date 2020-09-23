package vio

import (
	"errors"
	"io"
)

type zeroesReader struct {
}

func (rdr *zeroesReader) Read(p []byte) (n int, err error) {

	if len(p) == 0 {
		return
	}
	p[0] = 0
	for bp := 1; bp < len(p); bp *= 2 {
		copy(p[bp:], p[:bp])
	}

	return len(p), nil
}

var Zeroes = io.Reader(&zeroesReader{})

type writeSeeker struct {
	w io.Writer
	s io.Seeker
	k int64
}

func (ws *writeSeeker) Write(p []byte) (n int, err error) {
	n, err = ws.w.Write(p)
	if ws.s == nil {
		ws.k += int64(n)
	}
	return
}

func (ws *writeSeeker) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		if ws.s == nil {
			if offset < 0 {
				return 0, errors.New("streamio cannot go backwards")
			}
			k, err := io.CopyN(ws.w, Zeroes, offset)
			ws.k += k
			return ws.k, err
		} else {
			return ws.s.Seek(offset, whence)
		}
	case io.SeekStart:
		if ws.s == nil {
			whence = io.SeekCurrent
			offset = offset - ws.k
			return ws.Seek(offset, whence)
		} else {
			n, err := ws.s.Seek(offset+ws.k, whence)
			return n - ws.k, err
		}
	case io.SeekEnd:
		return 0, errors.New("streamio doesn't support io.SeekEnd")
	default:
		return 0, errors.New("invalid whence")
	}
}

func WriteSeeker(w io.Writer) (io.WriteSeeker, error) {

	ws := new(writeSeeker)
	ws.w = w

	if s, ok := w.(io.Seeker); ok {
		ws.s = s
		k, err := s.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, err
		}
		ws.k = k
	}

	return ws, nil

}
