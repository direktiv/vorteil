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
)

// Image file formats.
const (
	ImageFormatRAW = "raw"
)

type zeroes struct {
}

func (z *zeroes) Read(p []byte) (n int, err error) {

	if len(p) == 0 {
		return
	}

	p[0] = 0
	for bp := 1; bp < len(p); bp *= 2 {
		copy(p[bp:], p[:bp])
	}

	return len(p), nil
}

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

func (pio *partialIO) Seek(offset int64, whence int) (n int64, err error) {
	if pio.seeker != nil {
		n, err = pio.seeker.Seek(offset, whence)
		pio.offset = int(n)
		return
	}

	var aim int64
	switch whence {
	case io.SeekStart:
		aim = offset
	case io.SeekCurrent:
		aim = int64(pio.offset) + offset
	case io.SeekEnd:
		if pio.size < 0 {
			return int64(pio.offset), errors.New("underlying IO object does not know how long it will be")
		}
		aim = int64(pio.size) + offset
	}

	if aim < int64(pio.offset) {
		return int64(pio.offset), errors.New("underlying IO object does not support rewinding")
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
		k, err = io.CopyN(pio, &zeroes{}, aim-int64(pio.offset))
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
	format     string
	gptHeader  *GPTHeader
	gptEntries []*GPTEntry
	vmdk       vmdkInfo
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

// ImageFormat returns a string describing the image's file format.
func (iio *IO) ImageFormat() (format string, err error) {
	if iio.format != "" {
		return iio.format, nil
	}

	_, err = iio.src.Seek(0, io.SeekStart)
	if err != nil {
		return
	}

	var magic uint32

	err = binary.Read(iio.src, binary.LittleEndian, &magic)
	if err != nil {
		return
	}

	switch magic {
	case VMDKMagicNumber:
		header := new(VMDKHeader)
		header.MagicNumber = magic
		err = binary.Read(iio.src, binary.LittleEndian, &header.Fields)
		if err != nil {
			return
		}

		iio.vmdk.header = header

		switch header.Fields.Version {
		case 1:
			iio.format = ImageFormatVMDKSparse
			iio.img, err = iio.vmdkSparseIO()
			if err != nil {
				return
			}
		case 3:
			iio.format = ImageFormatVMDKStreamOptimized
			return iio.format, fmt.Errorf("stream-optimized VMDK not yet supported")
		default:
			err = fmt.Errorf("unsupported VMDK version: %d", header.Fields.Version)
			return
		}

	default:
		iio.format = ImageFormatRAW
		iio.img = iio.src
	}

	return iio.format, nil
}

// ..
const (
	SectorSize              = 512
	GPTHeaderLBA            = 1
	GPTHeaderSectors        = 1
	VorteilPartitionName    = "vorteil-os"
	FilesystemPartitionName = "vorteil-root"
)

// GPTHeader ..
type GPTHeader struct {
	Signature       [8]byte
	Revision        uint32
	HeaderSize      uint32
	HeaderCRC32     uint32
	_               uint32
	CurrentLBA      uint64
	BackupLBA       uint64
	FirstUsableLBA  uint64
	LastUsableLBA   uint64
	DiskGUID        [16]byte
	FirstEntriesLBA uint64
	TotalEntries    uint32
	EntrySize       uint32
	EntriesCRC32    uint32
}

// GPTEntry ..
type GPTEntry struct {
	TypeGUID   [16]byte
	UniqueGUID [16]byte
	FirstLBA   uint64
	LastLBA    uint64
	Attributes uint64
	Name       [72]byte
}

// NameString ..
func (e *GPTEntry) NameString() string {
	return cstringUTF16(e.Name[:])
}

// GPTHeader ..
func (iio *IO) GPTHeader() (*GPTHeader, error) {

	if iio.gptHeader != nil {
		return iio.gptHeader, nil
	}

	_, err := iio.img.Seek(GPTHeaderLBA*SectorSize, io.SeekStart)
	if err != nil {
		return nil, err
	}

	hdr := new(GPTHeader)

	err = binary.Read(iio.img, binary.LittleEndian, hdr)
	if err != nil {
		return nil, err
	}

	iio.gptHeader = hdr

	return iio.gptHeader, nil
}

// GPTEntries ..
func (iio *IO) GPTEntries() ([]*GPTEntry, error) {

	if iio.gptEntries != nil {
		return iio.gptEntries, nil
	}

	hdr, err := iio.GPTHeader()
	if err != nil {
		return nil, err
	}

	// PrintX(9, "first GPT entries LBA: %s", PrintableSize(int(hdr.FirstEntriesLBA)))

	_, err = iio.img.Seek(int64(hdr.FirstEntriesLBA*SectorSize), io.SeekStart)
	if err != nil {
		return nil, err
	}

	if hdr.EntrySize != 128 {
		return nil, fmt.Errorf("GPT uses abnormal entry size: %d", hdr.EntrySize)
	}

	list := make([]*GPTEntry, hdr.TotalEntries)
	for i := range list {
		entry := new(GPTEntry)
		err = binary.Read(iio.img, binary.LittleEndian, entry)
		if err != nil {
			return nil, err
		}
		list[i] = entry
	}

	iio.gptEntries = list

	return iio.gptEntries, nil
}

// GPTEntry ..
func (iio *IO) GPTEntry(name string) (*GPTEntry, error) {

	entries, err := iio.GPTEntries()
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if cstringUTF16(entry.Name[:]) == name {
			return entry, nil
		}
	}

	return nil, fmt.Errorf("partition entry not found: %s", name)
}

func (iio *IO) PartitionReader(name string) (io.Reader, error) {
	entry, err := iio.GPTEntry(FilesystemPartitionName)
	if err != nil {
		return nil, err
	}

	lbas := entry.LastLBA - entry.FirstLBA + 1
	start := entry.FirstLBA

	_, err = iio.img.Seek(int64(start)*SectorSize, io.SeekStart)
	if err != nil {
		return nil, err
	}

	return io.LimitReader(iio.img, int64(lbas)*SectorSize), nil
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

func cstringUTF16(data []byte) string {
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
