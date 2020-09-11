package gcparchive

import (
	"bytes"
	"errors"

	// "compress/gzip"
	"encoding/binary"
	"io"
	"strconv"

	"github.com/klauspost/compress/gzip"
	"github.com/vorteil/vorteil/pkg/vio"
)

type Sizer interface {
	Size() int64
}

type Writer struct {
	gz     *gzip.Writer
	length int64
	cursor int64
}

func NewWriter(w io.Writer, h Sizer) (*Writer, error) {

	gz, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
	if err != nil {
		return nil, err
	}

	gw := &Writer{
		length: h.Size(),
		gz:     gz,
	}

	err = gw.writeHeader()
	if err != nil {
		_ = gz.Close()
		return nil, err
	}

	return gw, nil

}

func (w *Writer) writeHeader() error {
	header := new(gcpTarPosixHeader)
	copy(header.Name[:], []byte("disk.raw")) // gcp requires the file is called disk.raw
	octal := "0" + strconv.FormatUint(uint64(w.length), 8)
	copy(header.Size[:], []byte(octal))
	header.Mode = [8]byte{0x30, 0x30, 0x30, 0x30, 0x36, 0x34, 0x34, 0x00}
	copy(header.Mtime[:], []byte("00000000000"))
	header.Typeflag = 0x30
	header.Version = [2]byte{0x20, 0x00}

	// Magic ...
	magic := []byte("ustar")
	byteMagic := make([]byte, len(magic)+1)
	for i := 0; i < len(byteMagic); i++ {
		if i == len(byteMagic)-1 {
			byteMagic[i] = byte(0x20)
		} else {
			byteMagic[i] = magic[i]
		}
	}
	copy(header.Magic[:], byteMagic)

	// TODO user/group info

	// Calculate Checksum ...
	headerBytes := &bytes.Buffer{}
	err := binary.Write(headerBytes, binary.LittleEndian, header)
	if err != nil {
		return err
	}
	bh := make([]byte, 512)
	err = binary.Read(headerBytes, binary.LittleEndian, &bh)
	if err != nil {
		return err
	}

	var csVal uint32
	csVal = 0
	for i := 0; i < len(bh); i++ {
		csVal += uint32(bh[i])
	}

	// Treat the 8 Checksum Field bytes as ASCII spaces (dec 32)
	csVal += (8 * 32)
	csOctal := "0" + strconv.FormatUint(uint64(csVal), 8)
	csBytes := []byte(csOctal)
	copy(header.Chksum[:], csBytes)
	header.Chksum[len(header.Chksum)-1] = 0x20

	err = binary.Write(w.gz, binary.LittleEndian, header)
	if err != nil {
		return err
	}

	return nil

}

func (w *Writer) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = w.cursor + offset
	case io.SeekEnd:
		abs = w.length + offset
	default:
		panic("bad seek whence")
	}

	if abs < w.cursor {
		return w.cursor, errors.New("gcp archive writer cannot seek backwards")
	}

	delta := abs - w.cursor
	_, err := io.CopyN(w, vio.Zeroes, delta)
	if err != nil {
		return w.cursor, err
	}

	return w.cursor, nil
}

func (w *Writer) Write(p []byte) (n int, err error) {

	n, err = w.gz.Write(p)
	w.cursor += int64(n)
	if err != nil {
		return
	}

	if w.cursor > w.length {
		return n, errors.New("gcp archive received more raw image data than was expected")
	}

	return
}

func (w *Writer) writeFooter() error {
	if w.cursor < w.length {
		return errors.New("gcp archive expected more raw image data than was received")
	}

	_, err := io.CopyN(w, vio.Zeroes, 15*512)
	if err != nil {
		return err
	}

	return nil
}

func (w *Writer) Close() error {
	var err error

	err = w.writeFooter()
	if err != nil {
		return err
	}

	err = w.gz.Close()
	if err != nil {
		return err
	}

	return nil
}

type gcpTarPosixHeader struct {
	Name       [100]byte // 0
	Mode       [8]byte   // 100
	UID        [8]byte   // 108
	GID        [8]byte   // 116
	Size       [12]byte  // 124
	Mtime      [12]byte  // 136
	Chksum     [8]byte   // 148
	Typeflag   byte      // 156
	Linkname   [100]byte // 157
	Magic      [6]byte   // 257
	Version    [2]byte   // 263
	Uname      [32]byte  // 265
	Gname      [32]byte  // 297
	Devmajor   [8]byte   // 329
	Devmintor  [8]byte   // 337
	Prefix     [155]byte // 345
	Endpadding [12]byte  // 500
	// 512
}
