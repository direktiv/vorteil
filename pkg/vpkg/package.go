package vpkg

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"hash/adler32"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/djherbis/buffer"
	"github.com/djherbis/nio"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vio"
)

// Suffix is the canonical file-extension given to Vorteil
// package files.
const Suffix = ".vorteil"

/*
The Vorteil package structure includes a small package header
containing information used to determine the correct way to
read the package. It starts with a "magic number" used to
identify that the file is indeed a vorteil package.

After the package header the remainder of the file is an
optionally compressed archive containing all of the components
needed to produce a Vorteil disk image.
*/
const magic = 0x004c494554524f56 // "VORTEIL "

type header struct {
	Magic        uint64
	VersionMajor uint8
	VersionMinor uint8
	VersionPatch uint8
	Pad          [501]byte
}

const headerLength = 512

// these path constants exist to standardize the names of
// critical package elements within Vorteil packages. They
// are named this way because the archiving logic orders
// them alphabetically, and we prefer the components to be
// extracted in this order for performance reasons.
const (
	vcfgPath = "./1.vcfg"
	iconPath = "./2.icon"
	fsPath   = "./4.fs"
)

// ..
const (
	SemverMajor    = 3
	SemverMinor    = 0
	SemverRevision = 0
)

// Hasher ..
type Hasher struct {
	hash.Hash32
}

// NewHasher ..
func NewHasher() *Hasher {
	return &Hasher{
		Hash32: adler32.New(),
	}
}

// String ..
func (h *Hasher) String() string {
	return fmt.Sprintf(hex.EncodeToString(h.Sum(nil)))
}

// Compression constants are defined here and copied from
// the standard library flate package so that code importing
// vpkg does not need to also import flate.
//
// The compression levels dictate how much work should be
// done to compress the contents of a package. Values other
// that those defined in these constants are acceptable,
// see the flate package documentation for for information.
const (
	NoCompression      = flate.NoCompression
	BestSpeed          = flate.BestSpeed
	BestCompression    = flate.BestCompression
	DefaultCompression = flate.BestSpeed
	HuffmanOnly        = flate.HuffmanOnly
)

// Builder defines a class of object that can be used to
// organize and then construct the contents of a new Vorteil
// package.
type Builder interface {
	Close() error

	// Pack writes the Vorteil package to the provided
	// io.Writer.
	Pack(w io.Writer) error

	// SetCompressionLevel is an advanced function that
	// can be used to adjust the amount of compression
	// that is done to the contents of the Vorteil
	// package. The default is DefaultCompression.
	SetCompressionLevel(level int)

	// SetMonitoringOptions is an advanced function that
	// can be used to add logging and progress reporting
	// to packaging operations, in addition to other
	// possible uses. See the documentation for the
	// MonitoringOptions object for more information.
	SetMonitoringOptions(opts MonitoringOptions)

	// SetVCFG takes the provided vio.File and uses it
	// as the vcfg for the package, overwriting any
	// previously existing vcfg.
	//
	// Note: this is NOT a vcfg merge operation. It
	// completely supercedes previous existing vcfgs.
	SetVCFG(f vio.File) error

	MergeVCFG(cfg *vcfg.VCFG) error

	// SetIcon takes the provided vio.File and uses it
	// as the icon for the package, overwriting any
	// previously existing icon.
	SetIcon(f vio.File) error

	// AddToFS takes the single vio.File and maps it
	// into the filesystem for the package, replacing
	// anything it conflicts with and automatically
	// creating parent directories on demand if required.
	//
	// Absolute and relative paths are both acceptable,
	// with relative paths being relative to the root
	// directory of the filesystem. For example, the
	// following are all equivalent:
	//
	//	dir/file
	//	/dir/file
	//	./dir/file
	AddToFS(path string, f vio.File) error

	// AddSubTreeToFS takes an entire vio.FileTree
	// and maps it into the filesystem for the package,
	// replacing anything it conflicts with and
	// automatically creating parent directories on
	// demand if required.
	//
	// Absolute and relative paths are both acceptable,
	// with relative paths being relative to the root
	// directory of the filesystem. For example, the
	// following are all equivalent:
	//
	//	dir/file
	//	/dir/file
	//	./dir/file
	AddSubTreeToFS(path string, sub vio.FileTree) error
}

type builder struct {
	tree             vio.FileTree
	vcfg             vio.File
	compressionLevel int
	monitoring       MonitoringOptions
	closeFunc        func() error
}

// NewBuilder returns an implementation of the Builder
// interface. The returned Builder will have an empty
// filesystem and no defined binary, vcfg, or icon. At a
// minimum, the Builder.SetBinary and Builder.SetVCFG functions
// must each be called once before the Builder.Pack function
// to constitute a valid and complete package.
func NewBuilder() Builder {

	mt, _ := time.ParseInLocation(time.RFC3339, "1970-01-01T00:00:00Z", time.UTC)

	b := &builder{
		tree:             vio.NewFileTree(),
		compressionLevel: DefaultCompression,
	}
	b.tree.Map(fsPath, vio.CustomFile(vio.CustomFileArgs{
		Name: filepath.Base(fsPath),
		Size: 0,
		// ModTime:    time.Unix(0, 0),
		ModTime:    mt,
		IsDir:      true,
		ReadCloser: ioutil.NopCloser(strings.NewReader("")),
	}))
	return b
}

// NewBuilderFromReader returns an implementation of the
// Builder interface with all of its internal components
// initialized to the values stored within an existing
// Vorteil package, as found in a Reader.
//
// If no further changes are made to the Builder or Reader,
// this package guarantees that the output of its Builder.Pack
// function will be identical to the input for the Load
// function that created the Reader.
func NewBuilderFromReader(rdr Reader) (Builder, error) {

	var err error
	b := NewBuilder()
	b.(*builder).closeFunc = rdr.Close

	err = b.SetVCFG(rdr.VCFG())
	if err != nil {
		return nil, err
	}

	err = b.SetIcon(rdr.Icon())
	if err != nil {
		return nil, err
	}

	err = rdr.FS().Walk(func(path string, f vio.File) error {
		return b.AddToFS(path, f)
	})
	if err != nil {
		return nil, err
	}

	return b, nil

}

func (b *builder) Close() error {
	if b.closeFunc != nil {
		b.closeFunc()
	}
	return b.tree.Close()
}

func (b *builder) SetVCFG(f vio.File) error {
	b.vcfg = f
	return b.tree.Map(vcfgPath, f)
}

func (b *builder) MergeVCFG(cfg *vcfg.VCFG) error {

	v, err := vcfg.LoadFile(b.vcfg)
	if err != nil {
		return err
	}

	err = v.Merge(cfg)
	if err != nil {
		return err
	}

	f, err := v.File()
	if err != nil {
		return err
	}

	err = b.SetVCFG(f)
	if err != nil {
		return err
	}

	return nil
}

func (b *builder) SetIcon(f vio.File) error {
	return b.tree.Map(iconPath, f)
}

func (b *builder) SetCompressionLevel(level int) {
	b.compressionLevel = level
}

func (b *builder) SetMonitoringOptions(opts MonitoringOptions) {
	b.monitoring = opts
}

func (b *builder) AddToFS(path string, f vio.File) error {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return errors.New("cannot add empty path to filesystem")
	}
	return b.tree.Map(fsPath+"/"+path, f)
}

func (b *builder) AddSubTreeToFS(path string, sub vio.FileTree) error {
	path = strings.TrimPrefix(path, "/")
	err := sub.Walk(func(p string, f vio.File) error {
		p = filepath.Clean(filepath.Join(path, p))
		return b.AddToFS(p, f)
	})
	return err
	// return b.tree.MapSubTree(fsPath+"/"+path, sub)
}

type multireader struct {
	io.Reader
	io.Closer
}

func (b *builder) Pack(w io.Writer) error {

	var err error

	err = b.monitoring.preprocess(b)
	if err != nil {
		return err
	}

	mw := b.monitoring.writer(w)

	hdr := new(header)
	hdr.Magic = magic
	hdr.VersionMajor = SemverMajor
	hdr.VersionMinor = SemverMinor
	hdr.VersionPatch = SemverRevision

	err = binary.Write(mw, binary.LittleEndian, hdr)
	if err != nil {
		return err
	}

	gz, err := gzip.NewWriterLevel(w, b.compressionLevel)
	if err != nil {
		return err
	}
	defer gz.Close()

	mw = b.monitoring.writer(gz)

	err = b.tree.Archive(mw, b.monitoring.archiveMonitoringFunc())
	if err != nil {
		return err
	}

	err = gz.Close()
	if err != nil {
		return err
	}

	return nil
}

// PreProcessReport contains information compiled by the
// packaging logic about an upcoming pack operation, before
// the packing actually begins. It is used only as an
// argument to the callback function at
// MonitoringOptions.PreProcessCompleteCallback, and is
// useful for initializing things like progress bars.
type PreProcessReport struct {
	NodeCount   int
	PackageSize int
}

// MonitoringOptions contains optional fields that may be
// provided in a call to Builder.SetMonitoringOptions to
// receive live information about a pack operation as it
// occurs.
//
// The PreProcessCompleteCallback, if provided, will be
// called precisely once. It will be called before anything
// is written to the PreCompressionWriter, and before any
// calls to the NextFileCallback. It provides the callback
// with information it has compiled about the contents of
// the Builder such as the total size of the uncompressed
// package, which can be helpful for keeping live progress
// tracking. If an error is returned the Builder.Pack
// function will fail, which means this callback can also
// be used to cancel a job if the PreProcessReport is
// unacceptable. If left nil, no such information is compiled
// and the Builder.Pack operation will be faster.
//
// NextFileCallback will be called once for each file and
// directory within the package. It is called just before
// that file is added to the archive, and provides
// information that can be used to report specifically what
// part of the packaging process the Builder.Pack logic has
// reached at any time. If an error is returned the
// Builder.Pack function will fail, which means this callback
// can also be used to cancel a job.
//
// If not nil, all data written to the package will be cloned
// to the PreCompressionWriter, uncompressed. The main
// forseen use for this is to track byte-by-byte how far
// along the complete Builder.Pack operation has come.
// Errors returned by the PreCompressionWriter will cause
// the Builder.Pack operation to fail, which means this
// writer can also be used to cancel a job.
type MonitoringOptions struct {
	PreProcessCompleteCallback func(report PreProcessReport) error
	NextFileCallback           func(path string, fi os.FileInfo) error
	PreCompressionWriter       io.Writer
}

func (opts *MonitoringOptions) preprocess(b Builder) error {
	if opts.PreProcessCompleteCallback == nil {
		return nil
	}

	report := new(PreProcessReport)
	report.PackageSize += headerLength
	report.PackageSize += 1024

	a := b.(*builder)
	err := a.tree.Walk(func(path string, f vio.File) error {
		report.NodeCount++
		report.PackageSize += ((f.Size()+512-1)/512)*512 + 512
		if len(path) > 100 {
			report.PackageSize += 1024
		}
		return nil
	})
	if err != nil {
		return err
	}

	err = opts.PreProcessCompleteCallback(*report)
	if err != nil {
		return err
	}

	return nil
}

func (opts *MonitoringOptions) writer(w io.Writer) io.Writer {
	if opts.PreCompressionWriter == nil {
		return w
	}
	return io.MultiWriter(w, opts.PreCompressionWriter)
}

func (opts *MonitoringOptions) archiveMonitoringFunc() vio.ArchiveFunc {
	if opts.NextFileCallback == nil {
		return nil
	}

	return func(path string, f vio.File) error {

		prefixes := []string{vcfgPath, iconPath, fsPath}
		for _, prefix := range prefixes {
			if strings.HasPrefix(path, prefix) {
				path = strings.TrimPrefix(path, prefix)
				break
			}
		}

		if path == "" {
			return nil
		}

		if path == "." {
			path = "/"
		}

		return opts.NextFileCallback(path, vio.Info(f))
	}
}

// ..
var (
	SupportedPackageMajor = 3
)

// ErrNotAPackage is returned when attempting to extract the
// contents of a file that not a Vorteil package, or at least
// is broken or corrupt.
var ErrNotAPackage = errors.New("not a valid package")

// ErrVersionNotSupported is returned when attempting to read an unsupported
// Vorteil package version.
var ErrVersionNotSupported = fmt.Errorf("package version not supported (require version %v.x.x)", SupportedPackageMajor)

// Reader defines a class of object that can be used to
// read specific information from a Vorteil package.
type Reader interface {

	// VCFG returns a vio.File object containing the
	// complete Vorteil configuration settings to be
	// used with the application.
	VCFG() vio.File

	// Icon returns a vio.File object containing a
	// picture file used as an icon representing the
	// package and application.
	//
	// This function will always returns a valid vio.File,
	// but packages commonly will not have an icon and
	// the calling logic should check the length of this
	// file (which will be zero in this circumstance) to
	// understand if an icon actually exists for the
	// package.
	Icon() vio.File

	// FS returns a vio.FileTree object representing
	// the total contents of the main filesystem on the
	// app's virtual disk.
	FS() vio.FileTree

	Close() error
}

type reader struct {
	closeFunc func() error
	vcfg      vio.File
	icon      vio.File
	fs        vio.FileTree
}

func (r *reader) Close() error {
	if r.closeFunc != nil {
		r.closeFunc()
	}
	r.vcfg.Close()
	if r.icon != nil {
		r.icon.Close()
	}
	r.fs.Close()
	return nil
}

func ReaderFromBuilder(b Builder) (Reader, error) {

	rdr := new(reader)
	rdr.closeFunc = b.Close

	bx, ok := b.(*builder)
	if !ok {
		r, w := nio.Pipe(buffer.New(0x100000))

		go func() {
			defer b.Close()
			b.SetCompressionLevel(NoCompression)
			err := b.Pack(w)
			if err != nil {
				w.CloseWithError(err)
				return
			}
			w.Close()
		}()

		return Load(r)
	}

	tree := bx.tree

	err := tree.Walk(func(path string, f vio.File) error {
		switch path {
		case vcfgPath:
			rdr.vcfg = f
		case iconPath:
			rdr.icon = f
		case fsPath:
			return vio.ErrSkip
		case ".":
			return nil
		default:
			return fmt.Errorf("unexpected archive element: %v", path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	rdr.fs, err = tree.SubTree(fsPath)
	if err != nil {
		return nil, err
	}

	return rdr, nil

}

// Load extracts information from the provided io.Reader
// and turns it into an implementation of the Reader interface,
// if the reader is a stream of valid Vorteil package data.
//
// This function loads information from the reader lazily,
// and in a predictable order that can be exploited by properly
// designed logic to minimize the amount of ram caching
// required without ever using temporary files.
//
// Because the Reader is loaded lazily, the provided io.Reader
// must remain valid for the lifetime of the Reader. If the
// provided io.Reader is also an io.Closer, it should NOT be
// closed until the reader is no longer required.
//
// The lazy loading won't necessarily consume the entire
// contents of the io.Reader up until EOF unless the calling
// logic makes use of the entire contents of the package.
// If it is important to consume the entire stream, you may
// want to io.Copy(ioutil.Discard, r) before closing it.
func Load(r io.Reader) (Reader, error) {

	var err error

	hdr := new(header)
	err = binary.Read(r, binary.LittleEndian, hdr)
	if err != nil {
		return nil, err
	}

	if hdr.Magic != magic {
		return nil, ErrNotAPackage
	}

	if hdr.VersionMajor != uint8(SupportedPackageMajor) {
		return nil, ErrVersionNotSupported
	}

	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tree, err := vio.LoadArchive(gz)
	if err != nil {
		return nil, err
	}

	rdr := new(reader)

	if closer, ok := r.(io.ReadCloser); ok {
		rdr.closeFunc = closer.Close
	}

	err = tree.Walk(func(path string, f vio.File) error {
		switch path {
		case vcfgPath:
			rdr.vcfg = f
		case iconPath:
			rdr.icon = f
		case fsPath:
			return vio.ErrSkip
		case ".":
			return nil
		default:
			return fmt.Errorf("unexpected archive element: %v", path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	rdr.fs, err = tree.SubTree(fsPath)
	if err != nil {
		return nil, err
	}

	return rdr, nil

}

// VCFG ..
func (r *reader) VCFG() vio.File {
	return r.vcfg
}

// Icon ..
func (r *reader) Icon() vio.File {

	if r.icon == nil {
		return vio.CustomFile(vio.CustomFileArgs{
			ReadCloser: ioutil.NopCloser(strings.NewReader("")),
		})
	}

	return r.icon
}

// FS ..
func (r *reader) FS() vio.FileTree {
	return r.fs
}

// ComputeHash ..
func ComputeHash(r io.Reader) (string, error) {

	hasher := NewHasher()

	rdr, err := Load(r)
	if err != nil {
		return "", err
	}

	bldr, err := NewBuilderFromReader(rdr)
	if err != nil {
		return "", err
	}

	bldr.SetMonitoringOptions(MonitoringOptions{
		PreCompressionWriter: hasher,
	})

	err = bldr.Pack(ioutil.Discard)
	if err != nil {
		return "", err
	}

	return hasher.String(), nil
}

type peekVCFGReader struct {
	vcfg     vio.File
	vcfgdata []byte
	Reader
}

// ReplaceVCFG ...
func ReplaceVCFG(r Reader, f vio.File) (Reader, error) {
	rdr, err := PeekVCFG(r)
	if err != nil {
		return nil, err
	}

	rdr.VCFG()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	x := rdr.(*peekVCFGReader)
	x.vcfgdata = data

	return x, nil
}

// PeekVCFG ..
func PeekVCFG(r Reader) (Reader, error) {
	rdr := new(peekVCFGReader)
	rdr.Reader = r
	return rdr, nil
}

func (rdr *peekVCFGReader) VCFG() vio.File {
	if rdr.vcfgdata == nil {
		f := rdr.Reader.VCFG()
		var err error
		rdr.vcfgdata, err = ioutil.ReadAll(f)
		if err != nil {
			panic(err)
		}

		rdr.vcfg = vio.CustomFile(vio.CustomFileArgs{
			Name:       f.Name(),
			Size:       f.Size(),
			ModTime:    f.ModTime(),
			IsDir:      f.IsDir(),
			IsSymlink:  f.IsSymlink(),
			ReadCloser: ioutil.NopCloser(bytes.NewReader(rdr.vcfgdata)),
		})

		return rdr.vcfg
	}

	return vio.CustomFile(vio.CustomFileArgs{
		Name:       rdr.vcfg.Name(),
		Size:       rdr.vcfg.Size(),
		ModTime:    rdr.vcfg.ModTime(),
		IsDir:      rdr.vcfg.IsDir(),
		IsSymlink:  rdr.vcfg.IsSymlink(),
		ReadCloser: ioutil.NopCloser(bytes.NewReader(rdr.vcfgdata)),
	})
}

func Open(path string) (Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	return Load(f)
}
