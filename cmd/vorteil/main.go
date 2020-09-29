package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sisatech/tablewriter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vio"
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

	p := log.NewProgress(fmt.Sprintf("Downloading %s", src), "%", 0)

	resp, err := http.Get(src)
	if err != nil {
		resp.Body.Close()
		p.Finish(false)
		return nil, err
	}
	p.Finish(true)

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
		return nil, fmt.Errorf("failed to resolve %s '%s'", argName, src)
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
	}
	return pkgb, err
}

func getBuilderDir(argName, src string) (vpkg.Builder, error) {
	var ptgt *vproj.Target
	path, target := vproj.Split(src)
	proj, err := vproj.LoadProject(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to resolve %s '%s'", argName, src)
	}

	ptgt, err = proj.Target(target)
	if err != nil {
		return nil, err
	}

	pkgb, err := ptgt.NewBuilder()
	return pkgb, err
}

func getPackageBuilder(argName, src string) (vpkg.Builder, error) {
	var err error
	var pkgB vpkg.Builder
	sType, err := getSourceType(src)
	if err != nil {
		return nil, err
	}

	switch sType {
	case sourceURL:
		pkgB, err = getBuilderURL(argName, src)
	case sourceFile:
		pkgB, err = getBuilderFile(argName, src)
	case sourceDir:
		pkgB, err = getBuilderDir(argName, src)
	case sourceINVALID:
		fallthrough
	default:
		err = fmt.Errorf("failed to resolve %s '%s'", argName, src)
	}

	return pkgB, err

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

// mergeFlagVCFGFiles : Merge values from from VCFG files stored in 'flagVCFG', and then merge vcfg flag values with overrideVCFG
func mergeVCFGFlagValues(b *vpkg.Builder) error {
	var err error
	var f vio.File
	var cfg *vcfg.VCFG

	// Iterate over vcfg paths stored in flagVCFG, read vcfg files and merge into b
	for _, path := range flagVCFG {
		f, err = vio.Open(path)
		if err != nil {
			return err
		}

		cfg, err = vcfg.LoadFile(f)
		if err != nil {
			return err
		}

		err = (*b).MergeVCFG(cfg)
		if err != nil {
			return err
		}
	}

	// Merge overrideVCFG object containing flag values into b
	err = (*b).MergeVCFG(&overrideVCFG)
	return err
}

func modifyPackageBuilder(b vpkg.Builder) error {
	var err error
	var f vio.File

	err = vcfgFlags.Validate()
	if err != nil {
		return err
	}

	if flagIcon != "" {
		f, err = vio.LazyOpen(flagIcon)
		if err != nil {
			return err
		}
		b.SetIcon(f)
	}

	err = mergeVCFGFlagValues(&b)
	if err != nil {
		return err
	}

	err = handleFileInjections(b)
	return err
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

// SetNumberModeFlagCMD : Will SetNumberMode to the value of the cmd flag 'numbers'
func SetNumberModeFlagCMD(cmd *cobra.Command) error {
	numbers, err := cmd.Flags().GetString("numbers")
	if err != nil {
		return err
	}

	err = SetNumbersMode(numbers)
	if err != nil {
		return fmt.Errorf("couldn't parse value of --numbers: %v", err)
	}

	return nil
}

// genericErrCheck : Very simple helper command to reduce duplication of err checks in the main package.
//	Exits program with given exit code and error if error is not nil
func genericErrCheck(err error, exitCode int) {
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(exitCode)
	}
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
