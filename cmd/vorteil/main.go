package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/sisatech/tablewriter"
	"github.com/sisatech/toml"
	"github.com/spf13/pflag"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/vkern"
	"github.com/vorteil/vorteil/pkg/vpkg"
	"github.com/vorteil/vorteil/pkg/vproj"
)

var (
	release = "0.0.0"
	commit  = ""
	date    = "Thu, 01 Jan 1970 00:00:00 +0000"
)

func main() {

	commandInit()

	err := rootCmd.Execute()

	if err != nil {
		os.Exit(1)
	}
}

func isEmptyDir(path string) bool {

	fis, err := ioutil.ReadDir(path)
	if err != nil {
		return false
	}

	if len(fis) > 0 {
		return false
	}

	return true
}

func isNotExist(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

func checkValidNewDirOutput(path string, force bool, dest, flag string) error {
	if !isEmptyDir(path) {
		return checkValidNewFileOutput(path, force, dest, flag)
	}

	return nil
}

func checkValidNewFileOutput(path string, force bool, dest, flag string) error {
	if !isNotExist(path) {
		if force {
			err := os.RemoveAll(path)
			if err != nil {
				return fmt.Errorf("failed to delete existing %s '%s': %w", dest, path, err)
			}

			dir := filepath.Dir(path)
			err = os.MkdirAll(dir, 0777)
			if err != nil {
				return fmt.Errorf("failed to create parent directory for %s '%s': %w", dest, path, err)
			}
		} else {
			return fmt.Errorf("%s '%s' already exists (you can use '%s' to force an overwrite)", dest, path, flag)
		}
	}

	return nil
}

func parseImageFormat(s string) (vdisk.Format, error) {
	format, err := vdisk.ParseFormat(s)
	if err != nil {
		return format, fmt.Errorf("%w -- try one of these: %s", err, strings.Join(vdisk.AllFormatStrings(), ", "))
	}
	return format, nil
}

type vorteildConf struct {
	KernelSources struct {
		Directory          string   `toml:"directory"`
		DropPath           string   `toml:"drop-path"`
		RemoteRepositories []string `toml:"remote-repositories"`
	} `toml:"kernel-sources"`
}

var ksrc vkern.Manager

func initKernels() error {

	home, err := homedir.Dir()
	if err != nil {
		return err
	}
	vorteild := filepath.Join(home, ".vorteild")
	conf := filepath.Join(vorteild, "conf.toml")
	var kernels, watch string
	var sources []string

	confData, err := ioutil.ReadFile(conf)
	if err != nil {
		kernels = filepath.Join(vorteild, "kernels")
		watch = filepath.Join(kernels, "watch")
		sources = []string{"https://downloads.vorteil.io/kernels"}
	} else {
		vconf := new(vorteildConf)
		err = toml.Unmarshal(confData, vconf)
		if err != nil {
			return err
		}
		kernels = vconf.KernelSources.Directory
		watch = vconf.KernelSources.DropPath
		sources = vconf.KernelSources.RemoteRepositories
	}

	err = os.MkdirAll(kernels, 0777)
	if err != nil {
		return err
	}

	err = os.MkdirAll(watch, 0777)
	if err != nil {
		return err
	}

	ksrc, err = vkern.CLI(vkern.CLIArgs{
		Directory:          kernels,
		DropPath:           watch,
		RemoteRepositories: sources,
	}, log)
	if err != nil {
		return err
	}

	vkern.Global = ksrc
	vimg.GetKernel = ksrc.Get
	vimg.GetLatestKernel = func(ctx context.Context) (vkern.CalVer, error) {
		s, err := ksrc.Latest()
		if err != nil {
			return vkern.CalVer(""), err
		}
		k, err := vkern.Parse(s)
		if err != nil {
			return vkern.CalVer(""), err
		}
		return k, nil
	}

	return nil

}

type sourceType string

const (
	sourceURL     sourceType = "URL"
	sourceFile               = "File"
	sourceDir                = "Dir"
	sourceINVALID            = "INVALID"
)

func getSourceType(src string) (sourceType, error) {
	var err error
	var fi os.FileInfo

	// Check if Source is a URL
	if _, err := url.ParseRequestURI(src); err == nil {
		if u, uErr := url.Parse(src); uErr == nil && u.Scheme != "" && u.Host != "" && u.Path != "" {
			return sourceURL, nil
		}
	}

	// Check if Source is a file or dir
	fi, err = os.Stat(src)
	if !os.IsNotExist(err) && (fi != nil && !fi.IsDir()) {
		return sourceFile, nil
	} else if !os.IsNotExist(err) && (fi != nil && fi.IsDir()) {
		return sourceDir, nil
	}

	// Source is unknown and thus is invalid
	return sourceINVALID, err
}

func getBuilderURL(argName, src string) (vpkg.Builder, error) {
	resp, err := http.Get(src)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}

	pkgr, err := vpkg.Load(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, err
	}

	pkgb, err := vpkg.NewBuilderFromReader(pkgr)
	if err != nil {
		resp.Body.Close()
		pkgr.Close()
		return nil, err
	}
	return pkgb, nil
}

func getBuilderFile(argName, src string) (vpkg.Builder, error) {
	f, err := os.Open(src)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	pkgr, err := vpkg.Load(f)
	if err != nil {
		f.Close()
		return nil, err
	}

	pkgb, err := vpkg.NewBuilderFromReader(pkgr)
	if err != nil {
		pkgr.Close()
		f.Close()
		return nil, err
	}
	return pkgb, nil
}

func getBuilderDir(argName, src string) (vpkg.Builder, error) {
	var ptgt *vproj.Target
	path, target := vproj.Split(src)
	proj, err := vproj.LoadProject(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	ptgt, err = proj.Target(target)
	if err != nil {
		return nil, err
	}

	pkgb, err := ptgt.NewBuilder()
	if err != nil {
		return nil, err
	}

	return pkgb, nil
}

func getPackageBuilder(argName, src string) (vpkg.Builder, error) {
	sType, err := getSourceType(src)
	if err != nil {
		return nil, err
	}

	switch sType {
	case sourceURL:
		return getBuilderURL(argName, src)
	case sourceFile:
		return getBuilderFile(argName, src)
	case sourceDir:
		return getBuilderDir(argName, src)
	case sourceINVALID:
		fallthrough
	default:
		return nil, fmt.Errorf("failed to resolve %s '%s'", argName, src)
	}

	// TODO: check for vrepo strings

}

var (
	flagIcon             string
	flagVCFG             []string
	flagInfoDate         string
	flagInfoURL          string
	flagSystemFilesystem string
	flagSystemOutputMode string
	flagSysctl           []string
	flagVMDiskSize       string
	flagVMInodes         string
	flagVMRAM            string
	overrideVCFG         vcfg.VCFG
)

func addModifyFlags(f *pflag.FlagSet) {
	vcfgFlags.AddTo(f)
}

func modifyPackageBuilder(b vpkg.Builder) error {

	// vcfg flags
	err := vcfgFlags.Validate()
	if err != nil {
		return err
	}

	// modify
	if flagIcon != "" {
		f, err := vio.LazyOpen(flagIcon)
		if err != nil {
			return err
		}
		b.SetIcon(f)
	}

	for _, path := range flagVCFG {
		f, err := vio.Open(path)
		if err != nil {
			return err
		}

		cfg, err := vcfg.LoadFile(f)
		if err != nil {
			return err
		}

		err = b.MergeVCFG(cfg)
		if err != nil {
			return err
		}
	}

	err = b.MergeVCFG(&overrideVCFG)
	if err != nil {
		return err
	}

	err = handleFileInjections(b)
	if err != nil {
		return err
	}

	return nil
}

// NumbersMode determines which numbers format a PrintableSize should render to.
var NumbersMode int

// SetNumbersMode parses s and sets NumbersMode accordingly.
func SetNumbersMode(s string) error {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)
	switch s {
	case "", "short":
		NumbersMode = 0
	case "dec", "decimal":
		NumbersMode = 1
	case "hex", "hexadecimal":
		NumbersMode = 2
	default:
		return fmt.Errorf("numbers mode must be one of 'dec', 'hex', or 'short'")
	}
	return nil
}

// PrintableSize is a wrapper around int to alter its string formatting behaviour.
type PrintableSize int

// String returns a string representation of the PrintableSize, formatted according to the global NumbersMode.
func (c PrintableSize) String() string {
	switch NumbersMode {
	case 0:
		x := int(c)
		if x == 0 {
			return "0"
		}
		var units int
		var suffixes = []string{"", "K", "M", "G"}
		for {
			if x%1024 != 0 {
				break
			}
			x /= 1024
			units++
			if units == len(suffixes)-1 {
				break
			}
		}
		return fmt.Sprintf("%d%s", x, suffixes[units])
	case 1:
		return fmt.Sprintf("%d", int(c))
	case 2:
		return fmt.Sprintf("%#x", int(c))
	default:
		panic("invalid NumbersMode")
	}
}

// PlainTable prints data in a grid, handling alignment automatically.
func PlainTable(vals [][]string) {
	if len(vals) == 0 {
		panic(errors.New("no rows provided"))
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetBorder(false)
	table.SetColumnSeparator("")
	for i := 1; i < len(vals); i++ {
		table.Append(vals[i])
	}

	table.Render()
}

func decompile(srcPath, outPath string) {
	iio, err := vdecompiler.Open(srcPath)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
	defer iio.Close()

	fi, err := os.Stat(outPath)
	if err != nil && !os.IsNotExist(err) {
		log.Errorf("%v", err)
		os.Exit(1)
	}
	var into bool
	if !os.IsNotExist(err) && fi.IsDir() {
		into = true
	}

	fpath := "/"
	dpath := outPath
	if into {
		dpath = filepath.ToSlash(filepath.Join(outPath, filepath.Base(fpath)))
	}

	var counter int

	symlinkCallbacks := make([]func() error, 0)

	var recurse func(int, string, string) error
	recurse = func(ino int, rpath string, dpath string) error {

		inode, err := iio.ResolveInode(ino)
		if err != nil {
			return err
		}

		if flagTouched && inode.LastAccessTime == 0 && !inode.IsDirectory() && rpath != "/" {
			log.Printf("skipping untouched object: %s", rpath)
			return nil
		}

		counter++

		log.Printf("copying %s", rpath)

		if inode.IsRegularFile() {
			fi, err = os.Stat(dpath)
			if !os.IsNotExist(err) {
				if err == nil {
					return fmt.Errorf("file already exists: %s", dpath)
				}
				return err
			}

			f, err := os.Create(dpath)
			if err != nil {
				return err
			}
			defer f.Close()

			rdr, err := iio.InodeReader(inode)
			if err != nil {
				return err
			}

			_, err = io.CopyN(f, rdr, int64(inode.Fullsize()))
			if err != nil {
				return err
			}
			return nil
		}

		if inode.IsSymlink() {

			symlinkCallbacks = append(symlinkCallbacks, func() error {
				rdr, err := iio.InodeReader(inode)
				if err != nil {
					return err
				}
				data, err := ioutil.ReadAll(rdr)
				if err != nil {
					return err
				}

				err = os.Symlink(string(string(data)), dpath)
				if err != nil {
					return err
				}
				return nil
			})
			return nil
		}

		if !inode.IsDirectory() {
			log.Warnf("skipping abnormal file: %s", rpath)
			return nil
		}

		fi, err = os.Stat(dpath)
		if !os.IsNotExist(err) {
			if err == nil {
				return fmt.Errorf("file already exists: %s", dpath)
			}
			return err
		}

		err = os.MkdirAll(dpath, 0777)
		if err != nil {
			return err
		}

		entries, err := iio.Readdir(inode)
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if entry.Name == "." || entry.Name == ".." {
				continue
			}
			err = recurse(entry.Inode, filepath.ToSlash(filepath.Join(rpath, entry.Name)), filepath.Join(dpath, entry.Name))
			if err != nil {
				return err
			}
		}

		return nil
	}

	ino, err := iio.ResolvePathToInodeNo(fpath)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
	err = recurse(ino, filepath.ToSlash(filepath.Base(fpath)), dpath)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}

	for _, fn := range symlinkCallbacks {
		err = fn()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
	}

	if flagTouched && counter <= 1 {
		log.Warnf("No touched files detected. Are you sure this disk has been run?")
	}
}
