package main

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/vkern"
	"github.com/vorteil/vorteil/pkg/vpkg"
	"github.com/vorteil/vorteil/pkg/vproj"
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
	if !isEmptyDir(path) && !isNotExist(path) {
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

	ksrc, err = vkern.Advanced(vkern.AdvancedArgs{
		Directory:          kernels,
		DropPath:           watch,
		RemoteRepositories: sources,
	})
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

func getPackageBuilder(argName, src string) (vpkg.Builder, error) {
	var isURL bool

	var pkgr vpkg.Reader
	var pkgb vpkg.Builder

	// check if src is a url
	if _, err := url.ParseRequestURI(src); err == nil {
		if u, uErr := url.Parse(src); uErr == nil && u.Scheme != "" && u.Host != "" && u.Path != ""{
			isURL = true
		}
	}

	// If src is a url, stream build package from remote src
	if isURL {
		resp, err := http.Get(src)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}

		pkgr, err = vpkg.Load(resp.Body)
		if err != nil {
			resp.Body.Close()
			return nil, err
		}

		pkgb, err = vpkg.NewBuilderFromReader(pkgr)
		if err != nil {
			resp.Body.Close()
			pkgr.Close()
			return nil, err
		}
		return pkgb, nil
	}

	// check for a package file
	fi, err := os.Stat(src)
	if !os.IsNotExist(err) && (fi != nil && !fi.IsDir()) {
		f, err := os.Open(src)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
		} else {
			pkgr, err = vpkg.Load(f)
			if err != nil {
				f.Close()
				return nil, err
			}

			pkgb, err = vpkg.NewBuilderFromReader(pkgr)
			if err != nil {
				pkgr.Close()
				f.Close()
				return nil, err
			}
			return pkgb, nil
		}
	}

	// check for a project directory
	var ptgt *vproj.Target
	path, target := vproj.Split(src)
	proj, err := vproj.LoadProject(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
	} else {
		ptgt, err = proj.Target(target)
		if err != nil {
			return nil, err
		}

		pkgb, err = ptgt.NewBuilder()
		if err != nil {
			return nil, err
		}

		return pkgb, nil
	}

	// TODO: check for urls
	// TODO: check for vrepo strings

	return nil, fmt.Errorf("failed to resolve %s '%s'", argName, src)
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
