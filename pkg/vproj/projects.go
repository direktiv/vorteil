package vproj

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/djherbis/buffer"
	"github.com/djherbis/nio"
	"github.com/gobwas/glob"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/vpkg"

	"github.com/sisatech/toml"
)

const (
	// FileName ..
	FileName          = ".vorteilproject"
	UnpackTempPattern = "vorteil-unpack-"
)

// TargetData ..
type TargetData struct {
	Name  string   `toml:"name" json:"name"`
	VCFGs []string `toml:"vcfgs,omitempty" json:"vcfgs"`
	Icon  string   `toml:"icon,omitempty" json:"icon"`
	Files []string `toml:"files,omitempty" json:"files"`
}

// ProjectData ..
type ProjectData struct {
	IgnorePatterns []string     `toml:"ignore" json:"ignore"`
	Targets        []TargetData `toml:"target,omitempty" json:"target"`
}

// Project ..
type Project struct {
	Dir     string
	Project ProjectData
}

// Marshal ..
func (p *ProjectData) Marshal() ([]byte, error) {
	buf := new(bytes.Buffer)

	err := toml.NewEncoder(buf).Encode(*p)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// LoadProject ..
func LoadProject(path string) (*Project, error) {

	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !fi.IsDir() {
		return nil, fmt.Errorf("'%s' is not a directory", path)
	}

	p := new(Project)
	p.Dir = path

	path = filepath.Join(path, FileName)
	fi, err = os.Stat(path)
	if err != nil {
		return nil, err
	}

	if fi.IsDir() {
		return nil, fmt.Errorf("'%s' is a directory", path)
	}

	if fi.Size() > 64*1024 {
		return nil, fmt.Errorf("'%s' is too large", path)
	}

	data, err := ioutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("no project in directory")
		}
		return nil, err
	}

	err = toml.Unmarshal(data, &p.Project)
	if err != nil {
		return nil, err
	}

	return p, nil

}

// Target ..
func (p *Project) Target(name string) (*Target, error) {

	targets := p.Project.Targets
	l := len(targets)

	t := new(Target)
	t.Dir = p.Dir
	t.Ignore = p.Project.IgnorePatterns

	var found bool

	for i := 0; i < l; i++ {
		if (i == 0 && name == "") || targets[i].Name == name {
			t.Name = targets[i].Name
			t.Icon = targets[i].Icon
			t.Files = targets[i].Files
			t.VCFGs = targets[i].VCFGs
			found = true
			break
		}
	}

	if !found {
		return nil, fmt.Errorf("project target '%s' not found", name)
	}

	return t, nil

}

// Target ..
type Target struct {
	Name   string
	Dir    string
	Ignore []string
	Icon   string
	VCFGs  []string
	Files  []string
}

// VCFG ..
func (t *Target) VCFG() (*vcfg.VCFG, error) {

	cfg := new(vcfg.VCFG)

	for i, path := range t.VCFGs {
		if !filepath.IsAbs(path) {
			path = filepath.Join(t.Dir, path)
		}
		err := cfg.LoadFilepath(path)
		if err != nil {
			if os.IsNotExist(err) {
				err = fmt.Errorf("vcfg '%s' not found", t.VCFGs[i])
			}
			return nil, err
		}
	}

	return cfg, nil
}

// NewBuilder ...
func (t *Target) NewBuilder() (vpkg.Builder, error) {

	b := vpkg.NewBuilder()

	vcfg, err := t.VCFG()
	if err != nil {
		return nil, err
	}

	x, err := vcfg.Marshal()
	if err != nil {
		return nil, err
	}

	cfg := vio.CustomFile(vio.CustomFileArgs{
		Name:       "default.vcfg",
		ReadCloser: ioutil.NopCloser(bytes.NewReader(x)),
		Size:       len(x),
		ModTime:    time.Now(),
	})

	err = b.SetVCFG(cfg)
	if err != nil {
		return nil, err
	}

	iconPath := t.Icon
	if iconPath != "" {
		if !filepath.IsAbs(iconPath) {
			iconPath = filepath.Join(t.Dir, t.Icon)
		}

		var icon vio.File
		if _, err := os.Stat(iconPath); err != nil {
			// icon not provided
			icon = vio.CustomFile(vio.CustomFileArgs{
				Name:       "default.png",
				ReadCloser: ioutil.NopCloser(bytes.NewReader([]byte{})),
				Size:       0,
				ModTime:    time.Now(),
			})
		} else {
			icon, err = vio.LazyOpen(iconPath)
			if err != nil {
				return nil, err
			}

			if icon.IsDir() {
				err = fmt.Errorf("icon file is a directory: %s", t.Icon)
				return nil, err
			}
		}

		err = b.SetIcon(icon)
		if err != nil {
			return nil, err
		}
	}

	ignore := make([]glob.Glob, 0)
	for _, p := range t.Ignore {
		ip, err := glob.Compile(p)
		if err != nil {
			return nil, err
		}

		ignore = append(ignore, ip)
	}

	t.Dir, err = filepath.Abs(t.Dir)
	if err != nil {
		return nil, err
	}
	t.Dir = filepath.ToSlash(t.Dir)

	err = filepath.Walk(t.Dir, func(path string, info os.FileInfo, err error) error {

		if err != nil {
			return err
		}

		path = filepath.ToSlash(path)
		abs := path
		path = strings.TrimPrefix(path, t.Dir)
		path = strings.TrimPrefix(path, "/")
		if path == "" {
			return nil
		}

		for _, ip := range ignore {
			if ip.Match(path) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		f, err := vio.LazyOpen(abs)
		if err != nil {
			return err
		}

		err = b.AddToFS(path, f)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	for _, d := range t.Files {
		abs := d
		if !filepath.IsAbs(d) {
			abs = filepath.Join(t.Dir, d)
		}

		abs = filepath.ToSlash(abs)

		err = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {

			if err != nil {
				return err
			}

			thisAbs := path
			path = filepath.ToSlash(path)
			path = strings.TrimPrefix(path, t.Dir)
			path = strings.TrimPrefix(path, "/")

			f, err := vio.LazyOpen(thisAbs)
			if err != nil {
				return err
			}

			if f.IsDir() {
				err = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {

					thisDir := path
					path = strings.TrimPrefix(path, abs)
					path = strings.TrimPrefix(path, "/")
					if path == "" {
						return nil
					}

					f, err := vio.LazyOpen(thisDir)
					if err != nil {
						return err
					}

					err = b.AddToFS(path, f)
					if err != nil {
						return err
					}

					return nil
				})
			} else {
				err = b.AddToFS(path, f)
				if err != nil {
					return err
				}

			}

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return b, nil
}

func vcfgInfo(cfg *vcfg.VCFG) (vio.File, error) {
	newVcfg := new(vcfg.VCFG)
	newVcfg.Info = cfg.Info
	data, err := newVcfg.Marshal()
	if err != nil {
		return nil, err
	}
	return vio.CustomFile(vio.CustomFileArgs{
		Size:       len(data),
		Name:       "readme.vcfg",
		ReadCloser: ioutil.NopCloser(bytes.NewReader(data)),
	}), nil
}

// FileWithoutInfo
func vcfgSansInfo(cfg *vcfg.VCFG, name string) (vio.File, error) {
	if name == "" {
		name = "default.vcfg"
	}
	newVcfg := new(vcfg.VCFG)
	newVcfg.Merge(cfg)
	newVcfg.Info = vcfg.PackageInfo{}
	data, err := newVcfg.Marshal()
	if err != nil {
		return nil, err
	}

	return vio.CustomFile(vio.CustomFileArgs{
		Size:       len(data),
		Name:       name,
		ReadCloser: ioutil.NopCloser(bytes.NewReader(data)),
	}), nil
}

func genericProjectData() ProjectData {
	return ProjectData{
		IgnorePatterns: []string{FileName},
		Targets: []TargetData{
			{
				Name:  "default",
				VCFGs: []string{"default.vcfg"},
				Icon:  "default.png",
				Files: []string{},
			},
		},
	}
}

// TarFromPackage writes a tar to w from the package reader
func TarFromPackage(w io.Writer, pkg vpkg.Reader) error {

	// Create tar writer
	tw := tar.NewWriter(w)
	defer tw.Close()

	// Load VCFG from package
	v, cfg, err := vcfgFromPkg(pkg)
	if err != nil {
		return err
	}
	defer cfg.Close()

	// Load icon from package
	iconName := "default.png"
	ico, iconFile, err := iconFromPkg(pkg)
	if err != nil {
		return err
	}
	defer ico.Close()
	defer os.Remove(iconFile.Name())

	// Load generic project data object
	var vprjIncluded bool
	vprj := genericProjectData()

	// Walk pkg filesystem
	files := make([]string, 0)
	err = walkFilesystem(pkg, files, tw, vprjIncluded, vprj)
	if err != nil {
		return err
	}

	if !vprjIncluded {
		// Write default icon/vcfg to tar
		err = defaultPNGAndVCFG(&defaultIconAndConfigArgs{v: v, vprj: vprj, tw: tw, ico: ico, iconFile: iconFile, iconName: iconName})
		if err != nil {
			return err
		}
	} else {
		// Write from the vorteil project definition
		err = tarFromVorteilProject(v, vprj, files, tw)
		if err != nil {
			return err
		}
	}

	return nil
}

// CreateFromPackage tars the reader and proceeds to unpack on path
func CreateFromPackage(path string, pkg vpkg.Reader) error {

	pr, pw := nio.Pipe(buffer.New(0x100000))

	go func() {
		err := TarFromPackage(pw, pkg)
		if err != nil {
			_ = pw.CloseWithError(err)
		}
		_ = pw.Close()
	}()

	defer pr.Close()

	tr := tar.NewReader(pr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		if hdr.FileInfo().Mode()&os.ModeSymlink > 0 {
			dpath := filepath.Join(path, hdr.Name)
			err = os.MkdirAll(filepath.Dir(dpath), 0777)
			if err != nil {
				return err
			}

			err = os.Symlink(hdr.Linkname, dpath)
			if err != nil {
				return err
			}

		} else if hdr.FileInfo().IsDir() {

			err = os.MkdirAll(filepath.Join(path, hdr.Name), os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}

		} else {
			err = os.MkdirAll(filepath.Join(path, filepath.Dir(hdr.Name)), os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}

			f, err := os.Create(filepath.Join(path, hdr.Name))
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = io.Copy(f, tr)
			if err != nil {
				return err
			}

			err = f.Close()
			if err != nil {
				return err
			}
		}

	}

	return nil

}

// Split takes src and returns the path and target of provided value
func Split(src string) (path string, target string) {
	src = filepath.ToSlash(src)
	parts := strings.Split(src, ":")
	if len(parts) == 1 {
		return src, ""
	}

	target = parts[len(parts)-1]
	path = strings.Join(parts[:len(parts)-1], ":")
	_, err := LoadProject(path)
	if err != nil {
		if os.IsNotExist(err) {
			return src, ""
		}
	}

	return
}
