package vproj

import (
	"archive/tar"
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"github.com/sisatech/toml"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/vpkg"
)

func vcfgFromPkg(pkg vpkg.Reader) (*vcfg.VCFG, vio.File, error) {

	var v *vcfg.VCFG
	var err error

	cfg := pkg.VCFG()
	defer func() {
		if err != nil {
			cfg.Close()
		}
	}()

	v, err = vcfg.LoadFile(cfg)
	if err != nil {
		return nil, nil, err
	}

	return v, cfg, nil
}

func iconFromPkg(pkg vpkg.Reader) (vio.File, *os.File, error) {

	var err error
	var iconFile *os.File

	ico := pkg.Icon()
	defer func() {
		if err != nil {
			ico.Close()
		}
	}()

	iconFile, err = ioutil.TempFile(os.TempDir(), UnpackTempPattern)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			iconFile.Close()
			os.Remove(iconFile.Name())
		}
	}()

	_, err = io.Copy(iconFile, ico)
	if err != nil {
		return nil, nil, err
	}

	return ico, iconFile, nil
}

func walkFilesystem(pkg vpkg.Reader, files []string, tw *tar.Writer, vprjIncluded bool, vprj ProjectData) error {

	return pkg.FS().Walk(func(path string, f vio.File) error {
		defer f.Close()
		defer io.Copy(ioutil.Discard, f)

		link, err := handleSymlink(f)
		if err != nil {
			return err
		}

		var mw io.Writer
		header, err := handleFile(mw, f, path, link, vprjIncluded, vprj, tw)
		if err != nil {
			return err
		}
		files = append(files, header.Name)

		return nil
	})
}

// if link is not empty, file is a symlink
func handleSymlink(f vio.File) (string, error) {
	var link string
	if f.IsSymlink() {
		data, err := ioutil.ReadAll(f)
		if err != nil {
			return link, err
		}
		link = string(data)
	}
	return link, nil
}

func handleFile(mw io.Writer, f vio.File, path, link string, vprjIncluded bool, vprj ProjectData, tw *tar.Writer) (*tar.Header, error) {

	header, err := tar.FileInfoHeader(vio.Info(f), link)
	if err != nil {
		return nil, err
	}

	var tf *os.File
	var x bool

	header.Name = strings.TrimPrefix(path, "./")
	if header.Name == FileName && !vprjIncluded {

		tf, err = ioutil.TempFile(os.TempDir(), UnpackTempPattern)
		if err != nil {
			return nil, err
		}
		defer os.Remove(tf.Name())
		defer tf.Close()
		x = true

		mw = io.MultiWriter(tf, tw)
		vprjIncluded = true

	} else {
		mw = tw
	}

	err = tw.WriteHeader(header)
	if err != nil {
		return nil, err
	}

	if !f.IsDir() {
		_, err = io.Copy(mw, f)
		if err != nil {
			return nil, err
		}
	}

	if x {
		err = readAndUnmarshalVprj(tf.Name(), &vprj)
		if err != nil {
			return nil, err
		}
	}

	return header, nil
}

func readAndUnmarshalVprj(name string, vprj *ProjectData) error {
	b, err := ioutil.ReadFile(name)
	if err != nil {
		return err
	}

	err = toml.Unmarshal(b, vprj)
	if err != nil {
		return err
	}

	return nil
}

func handleSplitVCFGs(v *vcfg.VCFG, tw *tar.Writer) error {

	cfgTmp, err := vcfgSansInfo(v, "")
	if err != nil {
		return err
	}
	defer cfgTmp.Close()

	err = writeFileToTar(tw, cfgTmp)
	if err != nil {
		return err
	}

	// write info field as readme.vcfg
	cfgTmp, err = vcfgInfo(v)
	if err != nil {
		return err
	}

	err = writeFileToTar(tw, cfgTmp)
	return err
}

func writeFileToTar(tw *tar.Writer, f vio.File) error {

	cfgHeader, err := tar.FileInfoHeader(vio.Info(f), "")
	if err != nil {
		return err
	}

	err = tw.WriteHeader(cfgHeader)
	if err != nil {
		return err
	}

	_, err = io.Copy(tw, f)
	if err != nil {
		return err
	}
	f.Close()

	return nil
}

func handleIcon(iconName string, ico vio.File, tw *tar.Writer, iconFile *os.File) error {
	if ico.Size() == 0 {
		return nil
	}
	_, err := iconFile.Seek(0, 0)
	if err != nil {
		return err
	}

	icoTmp := vio.CustomFile(vio.CustomFileArgs{
		ModTime:    ico.ModTime(),
		Name:       iconName,
		ReadCloser: iconFile,
		Size:       ico.Size(),
	})
	defer icoTmp.Close()

	icoHeader, err := tar.FileInfoHeader(vio.Info(icoTmp), "")
	if err != nil {
		return err
	}

	err = tw.WriteHeader(icoHeader)
	if err != nil {
		return err
	}

	_, err = io.Copy(tw, icoTmp)
	if err != nil {
		return err
	}

	return nil
}

type defaultIconAndConfigArgs struct {
	v        *vcfg.VCFG
	vprj     ProjectData
	tw       *tar.Writer
	ico      vio.File
	iconFile *os.File
	iconName string
}

func defaultPNGAndVCFG(args *defaultIconAndConfigArgs) error {

	err := handleSplitVCFGs(args.v, args.tw)
	if err != nil {
		return err
	}

	err = handleIcon(args.iconName, args.ico, args.tw, args.iconFile)
	if err != nil {
		return err
	}

	for i := range args.vprj.Targets {
		if args.vprj.Targets[i].Name == "default" {
			args.vprj.Targets[i].VCFGs = append(args.vprj.Targets[i].VCFGs, "readme.vcfg")
		}
	}

	err = handleVprjTargetFile(args.vprj, args.tw)
	if err != nil {
		return err
	}

	return nil
}

func handleVprjTargetFile(vprj ProjectData, tw *tar.Writer) error {

	b, err := vprj.Marshal()
	if err != nil {
		return err
	}

	vprjTmp := vio.CustomFile(vio.CustomFileArgs{
		ModTime:    time.Now(),
		Name:       FileName,
		ReadCloser: ioutil.NopCloser(bytes.NewReader(b)),
		Size:       len(b),
	})
	defer vprjTmp.Close()

	err = writeFileToTar(tw, vprjTmp)
	if err != nil {
		return err
	}

	return nil
}

func tarFromVorteilProject(v *vcfg.VCFG, vprj ProjectData, files []string, tw *tar.Writer) error {
	for _, x := range vprj.Targets[0].VCFGs {
		for _, s := range files {
			if s == FileName {

				for i := range vprj.Targets {
					if vprj.Targets[i].Name == strings.TrimSuffix(x, path.Ext(x)) {
						vprj.Targets[i].VCFGs = append(vprj.Targets[i].VCFGs, "readme.vcfg")
					}
				}

				err := handleVprjTargetFile(vprj, tw)
				if err != nil {
					return err
				}
			}
			if s == x {
				continue
			}

			// HandleSplitVCFGs ...
			err := handleSplitVCFGs(v, tw)
			if err != nil {
				return err
			}

		}
	}

	return nil
}
