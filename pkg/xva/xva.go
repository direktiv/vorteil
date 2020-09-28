package xva

import (
	"archive/tar"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vio"
)

// Sizer is an interface that shouldn't exist in a vacuum, but does because our
// other image formats follow a similar patten and need more information. A
// Sizer should return the true and final RAW size of the image and be callable
// before the first byte of data is written to the Writer. Note that our
// vimg.Builder implements this interface and is the intended argument in most
// cases.
type Sizer interface {
	Size() int64
}

// Writer implements io.Closer, io.Writer, and io.Seeker interfaces. Creating an
// XVA image is as simple as getting one of these writers and copying a raw
// image into it.
type Writer struct {
	tw  *tar.Writer
	h   Sizer
	cfg *vcfg.VCFG

	hdr    *tar.Header
	hasher hash.Hash
	buffer *bytes.Buffer
	cursor int64
}

// NewWriter returns a Writer to which a RAW image can be copied in order to
// create an XVA format disk image. The Sizer 'h' must accurately return the
// true and final RAW size of the image.
func NewWriter(w io.Writer, h Sizer, cfg *vcfg.VCFG) (*Writer, error) {

	xw := new(Writer)
	xw.h = h
	xw.cfg = cfg
	xw.tw = tar.NewWriter(w)

	err := xw.writeOVAXML()
	if err != nil {
		_ = xw.tw.Close()
		return nil, err
	}

	xw.hasher = sha1.New()
	xw.buffer = new(bytes.Buffer)

	return xw, nil

}

func (w *Writer) writeOVAXML() error {

	// timestamp := src.ModTime()
	timestamp := time.Now()

	hdr := &tar.Header{
		ModTime:    timestamp,
		AccessTime: timestamp,
		ChangeTime: timestamp,
	}
	w.hdr = hdr

	// write ova.xml
	hdr.Name = "ova.xml"
	ova := w.ovaXML()
	hdr.Size = int64(len(ova))

	err := w.tw.WriteHeader(hdr)
	if err != nil {
		return err
	}

	_, err = io.Copy(w.tw, strings.NewReader(ova))
	if err != nil {
		return err
	}

	return nil

}

const mib = 0x100000
const emptyChunkChecksum = "3b71f43ff30f4b15b5cd85dd9e95ebc7e84eb5a3"

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (n int, err error) {

	var total int

	for {
		chunkSpace := mib - w.cursor%mib
		if int64(len(p)) < chunkSpace {
			n, err = w.buffer.Write(p)
			w.cursor += int64(n)
			total += n
			return total, err
		}

		this := p[:chunkSpace]
		next := p[chunkSpace:]
		n, err = w.buffer.Write(this)
		w.cursor += int64(n)
		total += n
		if err != nil {
			return total, err
		}

		err = w.flushBuffer()
		if err != nil {
			return total, err
		}

		if len(next) > 0 {
			p = next
			continue
		}

		break
	}

	return total, err

}

func (w *Writer) flushChunkHeader(chunk int64) error {

	w.hdr.Name = filepath.Join("Ref:4", fmt.Sprintf("%08d", chunk))
	w.hdr.Size = int64(mib)
	err := w.tw.WriteHeader(w.hdr)
	if err != nil {
		return err
	}

	_, err = io.Copy(w.tw, bytes.NewReader(w.buffer.Bytes()))
	if err != nil {
		return err
	}

	return nil

}

func (w *Writer) flushChunkData(checksum string) error {

	w.hdr.Name += ".checksum"
	w.hdr.Size = 40
	err := w.tw.WriteHeader(w.hdr)
	if err != nil {
		return err
	}

	_, err = io.Copy(w.tw, strings.NewReader(checksum))
	if err != nil {
		return err
	}

	return nil

}

func (w *Writer) flushBuffer() error {

	chunk := w.cursor/mib - 1
	checksum := hex.EncodeToString(w.hasher.Sum(nil))
	if checksum != emptyChunkChecksum {
		err := w.flushChunkHeader(chunk)
		if err != nil {
			return err
		}

		err = w.flushChunkData(checksum)
		if err != nil {
			return err
		}
	}

	w.buffer.Reset()
	w.hasher.Reset()
	return nil

}

// Seek implements io.Seeker
func (w *Writer) Seek(offset int64, whence int) (int64, error) {

	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = w.cursor + offset
	case io.SeekEnd:
		abs = w.h.Size() + offset
	default:
		panic("bad seek whence")
	}

	if abs < w.cursor {
		return w.cursor, errors.New("xva archive writer cannot seek backwards")
	}

	// TODO: make this faster using HolePredictor
	delta := abs - w.cursor
	_, err := io.CopyN(w, vio.Zeroes, delta)
	if err != nil {
		return w.cursor, err
	}

	return w.cursor, nil

}

// Close implements io.Closer
func (w *Writer) Close() error {

	if w.cursor < w.h.Size() {
		return errors.New("xva archive expected more raw image data than was received")
	}

	err := w.tw.Close()
	if err != nil {
		return err
	}

	return nil

}

func (w *Writer) ovaXML() string {

	cfg := w.cfg

	name := cfg.Info.Name
	if name == "" {
		name = "Vorteil App"
	}
	description := cfg.Info.Summary
	if description == "" {
		description = "Created by Vorteil"
	}
	mem := int(cfg.VM.RAM)
	cpus := int(cfg.VM.CPUs)

	var networkVIFs string
	var networkSettings string
	for i := range cfg.Networks {
		vifID := 2*i + 8
		netID := 2*i + 9
		if i == 0 {
			vifID = 1
			netID = 2
		}
		mtu := cfg.Networks[i].MTU
		if mtu == 0 {
			mtu = 1500
		}
		networkVIFs += fmt.Sprintf(networkVIFTemplate, vifID)
		networkSettings += fmt.Sprintf(networkSettingsTemplate, vifID, i, netID, mtu, netID, vifID, mtu)
	}

	s := fmt.Sprintf(ovaXMLTemplate, name, description, mem, mem, mem, mem, cpus, cpus, networkVIFs, networkSettings, w.h.Size())

	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		lines[i] = strings.TrimSpace(lines[i])
	}
	s = strings.Join(lines, "")

	return s

}

const networkVIFTemplate = `<value>Ref:%d</value>`
