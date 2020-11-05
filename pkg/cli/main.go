package cli

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sisatech/tablewriter"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vorteil/vorteil/pkg/elog"
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

// Each command executed may have a error message and status code
var errorStatusCode int
var errorStatusMessage error

// SetError sets the global variables for when the process exits to display accordingly
func SetError(err error, code int) {
	errorStatusCode = code
	errorStatusMessage = err
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

// readSourcePath fetches src, target or returns an error
func readSourcePath(src string) (string, string, error) {

	// Get absolute path to help with splitting
	src, err := filepath.Abs(src)
	if err != nil {
		return "", "", err
	}

	// Check if source contains a target
	colonSplit := strings.Split(src, ":")
	colonLength := len(colonSplit)

	// Most of the time target should be here
	target := colonSplit[len(colonSplit)-1]

	// If target is src no target was provided
	if target == src {
		target = ""
	}
	if target != "" {
		src = colonSplit[0]
		if runtime.GOOS == "windows" {
			src = fmt.Sprintf("%s:%s", colonSplit[0], colonSplit[1])
			// Catches edge case when window users provide no targets because of directory
			if colonLength == 2 {
				target = ""
			}
		}
	}

	return src, target, err
}

func getSourceType(src string) (sourceType, error) {
	var err error
	var fi os.FileInfo

	// Check if Source is a URL
	if _, err := url.ParseRequestURI(src); err == nil {
		if u, uErr := url.Parse(src); uErr == nil && u.Scheme != "" && u.Host != "" && u.Path != "" {
			return sourceURL, nil
		}
	}

	src, target, err := readSourcePath(src)
	if err != nil {
		return sourceINVALID, err
	}

	// Check if Source is a file or dir
	fi, err = os.Stat(src)
	if !os.IsNotExist(err) && (fi != nil && !fi.IsDir()) {
		if target != "" {
			return sourceINVALID, errors.New("Targetable runs are unable to be used on packages")
		}
		return sourceFile, nil
	} else if !os.IsNotExist(err) && (fi != nil && fi.IsDir()) {
		return sourceDir, nil
	}

	// Source is unknown and thus is invalid
	return sourceINVALID, err
}

func checkIfNewVRepo(src string) (string, error) {
	urlo, err := url.Parse(src)
	if err != nil {
		return "", err
	}
	client := &http.Client{}

	// TODO Need to find a way to remove metadata from future vrepo urls
	req, err := http.NewRequest("GET", fmt.Sprintf("%s://%s/metadata/info", urlo.Scheme, urlo.Host), nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("not a new vorteil repository")
	}
	return resp.Header.Get("Vorteil-Repository"), nil
}

func getReaderURL(src string) (vpkg.Reader, error) {

	newVrepo, err := checkIfNewVRepo(src)
	if err != nil {
		return nil, err
	}
	client := &http.Client{}

	req, err := http.NewRequest("GET", src, nil)
	if err != nil {
		return nil, err
	}
	if newVrepo == "True" {
		token, err := checkAuthentication()
		if err != nil {
			return nil, err
		}

		if token != "" {
			req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		if resp != nil {
			resp.Body.Close()
		}
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}

	var p elog.Progress
	if resp.ContentLength == -1 {
		p = log.NewProgress("Downloading package", "", 0)
		defer p.Finish(true)
	} else {
		p = log.NewProgress("Downloading package", "KiB", resp.ContentLength)
	}

	pkgr, err := vpkg.Load(p.ProxyReader(resp.Body))
	if err != nil {
		resp.Body.Close()
		p.Finish(false)
		return nil, err
	}

	return pkgr, nil
}

func getBuilderURL(argName, src string) (vpkg.Builder, error) {
	pkgr, err := getReaderURL(src)
	if err != nil {
		return nil, err
	}
	pkgb, err := vpkg.NewBuilderFromReader(pkgr)
	if err != nil {
		pkgr.Close()
		return nil, err
	}
	return pkgb, nil
}

func getReaderFile(src string) (vpkg.Reader, error) {
	f, err := os.Open(src)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		return nil, fmt.Errorf("failed to resolve %s ", src)
	}

	pkgr, err := vpkg.Load(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	return pkgr, nil
}
func getBuilderFile(argName, src string) (vpkg.Builder, error) {

	pkgr, err := getReaderFile(src)
	if err != nil {
		return nil, err
	}
	pkgb, err := vpkg.NewBuilderFromReader(pkgr)
	if err != nil {
		pkgr.Close()
		return nil, err
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

func getPackageReader(argName, src string) (vpkg.Reader, error) {
	var err error
	var pkgR vpkg.Reader
	sType, err := getSourceType(src)
	if err != nil {
		return nil, err
	}

	switch sType {
	case sourceURL:
		pkgR, err = getReaderURL(src)
	case sourceFile:
		pkgR, err = getReaderFile(src)
	case sourceINVALID:
		fallthrough
	default:
		err = fmt.Errorf("failed to resolve %s '%s'", argName, src)
	}

	return pkgR, err
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
