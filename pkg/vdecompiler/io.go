package vdecompiler

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"unicode/utf16"

	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/vmdk"
)

// Partial IO errors, for when attempting to perform an operation that
// would be legal on a file but impossible on a read-only stream.
var (
	ErrRead  = errors.New("underlying IO object does not support reading")
	ErrSeek  = errors.New("underlying IO object does not support seeking")
	ErrWrite = errors.New("underlying IO object does not support writing")
)

type partialIO struct {
	name   string
	offset int
	size   int
	reader io.Reader
	closer io.Closer
	seeker io.Seeker
	writer io.Writer
}

func (pio *partialIO) Read(p []byte) (n int, err error) {
	if pio.reader == nil {
		return 0, fmt.Errorf("reading from %s: %w", pio.name, ErrRead)
	}
	n, err = pio.reader.Read(p)
	pio.offset += n
	return
}

func (pio *partialIO) Close() error {
	if pio.closer == nil {
		return nil
	}
	return pio.closer.Close()
}

func (pio *partialIO) Write(p []byte) (n int, err error) {
	if pio.writer == nil {
		return 0, fmt.Errorf("writing to %s: %w", pio.name, ErrWrite)
	}
	n, err = pio.writer.Write(p)
	pio.offset += n
	return
}

func (pio *partialIO) calculateAim(offset int64, whence int) (int64, error) {

	var aim int64
	switch whence {
	case io.SeekStart:
		aim = offset
	case io.SeekCurrent:
		aim = int64(pio.offset) + offset
	case io.SeekEnd:
		if pio.size < 0 {
			return 0, errors.New("underlying IO object does not know how long it will be")
		}
		aim = int64(pio.size) + offset
	}

	if aim < int64(pio.offset) {
		return 0, errors.New("underlying IO object does not support rewinding")
	}

	return aim, nil

}

func (pio *partialIO) Seek(offset int64, whence int) (n int64, err error) {

	if pio.seeker != nil {
		n, err = pio.seeker.Seek(offset, whence)
		pio.offset = int(n)
		return
	}

	aim, err := pio.calculateAim(offset, whence)
	if err != nil {
		n = int64(pio.offset)
		return
	}

	if pio.reader != nil {
		var k int64
		k, err = io.CopyN(ioutil.Discard, pio, aim-int64(pio.offset))
		pio.offset += int(k)
		if err == io.EOF {
			err = nil
		}
		n = int64(pio.offset)
		return
	}

	if pio.writer != nil {
		var k int64
		k, err = io.CopyN(pio, vio.Zeroes, aim-int64(pio.offset))
		pio.offset += int(k)
		if err == io.EOF {
			err = nil
		}
		n = int64(pio.offset)
		return
	}

	panic("No seeker, reader, or writer?")

}

// IO provides an entry point into a virtual disk image, making it
// possible to navigate and read data from it. It has a complex but
// flexible implementation, allowing it to work from both seekable files
// and read-only streams.
type IO struct {
	src, img   *partialIO
	format     vdisk.Format
	gptHeader  *vimg.GPTHeader
	gptEntries []*vimg.GPTEntry
	vmdk       *vmdk.Header
	vpart      vpartInfo
	fs         fsInfo
}

// Close closes the underlying IO object and cleans up any other resources
// in use.
func (iio *IO) Close() error {
	return iio.src.Close()
}

type imageIOLoader struct {
	iio *IO
}

func (l *imageIOLoader) Close() error {
	_, err := l.iio.ImageFormat()
	if err != nil {
		return fmt.Errorf("could not initialize image IO: %w", err)
	}
	return l.iio.img.Close()
}

func (l *imageIOLoader) Read(p []byte) (n int, err error) {
	_, err = l.iio.ImageFormat()
	if err != nil {
		return 0, fmt.Errorf("could not initialize image IO: %w", err)
	}
	return l.iio.img.Read(p)
}

func (l *imageIOLoader) Seek(offset int64, whence int) (n int64, err error) {
	_, err = l.iio.ImageFormat()
	if err != nil {
		return 0, fmt.Errorf("could not initialize image IO: %w", err)
	}
	return l.iio.img.Seek(offset, whence)
}

func (l *imageIOLoader) Write(p []byte) (n int, err error) {
	_, err = l.iio.ImageFormat()
	if err != nil {
		return 0, fmt.Errorf("could not initialize image IO: %w", err)
	}
	return l.iio.img.Write(p)
}

func newIO(srcName string, srcSize int, img interface{}) (*IO, error) {

	iio := new(IO)
	iio.src = new(partialIO)
	iio.src.name = srcName
	iio.src.size = srcSize
	iio.src.closer, _ = img.(io.Closer)
	iio.src.reader, _ = img.(io.Reader)
	iio.src.seeker, _ = img.(io.Seeker)
	iio.src.writer, _ = img.(io.Writer)

	iio.img = new(partialIO)
	imgLoader := &imageIOLoader{iio: iio}
	iio.img.closer = imgLoader
	iio.img.reader = imgLoader
	iio.img.seeker = imgLoader
	iio.img.writer = imgLoader

	return iio, nil

}

// Open returns an image IO object from a file at path.
func Open(path string) (*IO, error) {

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	iio, err := newIO(path, int(fi.Size()), f)
	if err != nil {
		f.Close()
		return nil, err
	}

	return iio, nil

}

func (iio *IO) resolveVMDKFormat(buf []byte) error {

	header := new(vmdk.Header)
	err := binary.Read(bytes.NewReader(buf), binary.LittleEndian, header)
	if err != nil {
		return err
	}

	iio.vmdk = header

	switch iio.vmdk.Version {
	case 1:
		iio.format = vdisk.VMDKSparseFormat
		iio.img, err = iio.vmdkSparseIO()
	case 3:
		iio.format = vdisk.VMDKStreamOptimizedFormat
		err = fmt.Errorf("stream-optimized VMDK not yet supported")
	default:
		err = fmt.Errorf("unsupported VMDK version: %d", iio.vmdk.Version)
	}

	return err

}

func (iio *IO) determineImageFormat() error {

	_, err := iio.src.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	_, err = io.CopyN(buf, iio.src, 512)
	if err != nil {
		return err
	}

	var magic uint32

	err = binary.Read(bytes.NewReader(buf.Bytes()), binary.LittleEndian, &magic)
	if err != nil {
		return err
	}

	switch magic {
	case uint32(vmdk.Magic):
		err = iio.resolveVMDKFormat(buf.Bytes())
	default:
		iio.format = vdisk.RAWFormat
		iio.img = iio.src
	}

	return err

}

// ImageFormat returns the image's file format.
func (iio *IO) ImageFormat() (vdisk.Format, error) {

	if iio.format != "" {
		return iio.format, nil
	}

	err := iio.determineImageFormat()
	if err != nil {
		return iio.format, err
	}

	return iio.format, nil

}

// GPTEntryName returns a normal string representation of the GPT entry. Without
// calling this function the data in the GPT entry is encoded in UTF16.
func GPTEntryName(e *vimg.GPTEntry) string {
	return UTF16toString(e.Name[:])
}

func (iio *IO) readGPTHeader() error {

	_, err := iio.img.Seek(vimg.PrimaryGPTHeaderLBA*vimg.SectorSize, io.SeekStart)
	if err != nil {
		return err
	}

	hdr := new(vimg.GPTHeader)

	err = binary.Read(iio.img, binary.LittleEndian, hdr)
	if err != nil {
		return err
	}

	iio.gptHeader = hdr

	if hdr.SizePartEntry != vimg.GPTEntrySize {
		return fmt.Errorf("GPT uses abnormal entry size: %d", hdr.SizePartEntry)
	}

	return nil

}

// GPTHeader returns the primary GPT header for the image.
func (iio *IO) GPTHeader() (*vimg.GPTHeader, error) {

	if iio.gptHeader != nil {
		return iio.gptHeader, nil
	}

	err := iio.readGPTHeader()
	if err != nil {
		return nil, err
	}

	return iio.gptHeader, nil

}

func (iio *IO) readGPTEntries() error {

	hdr, err := iio.GPTHeader()
	if err != nil {
		return err
	}

	_, err = iio.img.Seek(int64(hdr.StartLBAParts*vimg.SectorSize), io.SeekStart)
	if err != nil {
		return err
	}

	list := make([]*vimg.GPTEntry, hdr.NoOfParts)
	for i := range list {
		entry := new(vimg.GPTEntry)
		err = binary.Read(iio.img, binary.LittleEndian, entry)
		if err != nil {
			return err
		}
		list[i] = entry
	}

	iio.gptEntries = list

	return nil

}

// GPTEntries returns a list of all GPT partition entries on the disk.
func (iio *IO) GPTEntries() ([]*vimg.GPTEntry, error) {

	if iio.gptEntries != nil {
		return iio.gptEntries, nil
	}

	err := iio.readGPTEntries()
	if err != nil {
		return nil, err
	}

	return iio.gptEntries, nil

}

// GPTEntry returns the GPT entry for a specific partition on-disk.
func (iio *IO) GPTEntry(name string) (*vimg.GPTEntry, error) {

	entries, err := iio.GPTEntries()
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if UTF16toString(entry.Name[:]) == name {
			return entry, nil
		}
	}

	return nil, fmt.Errorf("partition entry not found: %s", name)

}

// PartitionReader returns a limited reader for the an entire disk partition.
// Valid arguments are vimg.RootPartitionName and vimg.OSPartitionName. This
// function can be used to easily extract the file-system from a Vorteil image.
func (iio *IO) PartitionReader(name string) (io.Reader, error) {

	entry, err := iio.GPTEntry(name)
	if err != nil {
		return nil, err
	}

	lbas := entry.LastLBA - entry.FirstLBA + 1
	start := entry.FirstLBA

	_, err = iio.img.Seek(int64(start)*vimg.SectorSize, io.SeekStart)
	if err != nil {
		return nil, err
	}

	return io.LimitReader(iio.img, int64(lbas)*vimg.SectorSize), nil

}

func cstring(data []byte) string {

	var s string
	s = string(data[:])
	for i := 0; i < len(data); i++ {
		if data[i] == 0 {
			s = string(data[:i])
			break
		}
	}

	return s

}

func UTF16toString(data []byte) string {

	if len(data)%2 != 0 {
		panic("string length makes UTF16 impossible")
	}

	var x []uint16
	x = make([]uint16, len(data)/2)
	err := binary.Read(bytes.NewReader(data), binary.LittleEndian, x)
	if err != nil {
		panic(err)
	}

	s := string(utf16.Decode(x))
	for i := range s {
		if s[i] == 0 {
			s = s[:i]
			break
		}
	}

	return s

}
