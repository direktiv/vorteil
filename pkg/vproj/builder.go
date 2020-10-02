package vproj

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gobwas/glob"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/vpkg"
)

// NewBuilder ...
func (t *Target) NewBuilder() (vpkg.Builder, error) {

	b := vpkg.NewBuilder()
	ignore := make([]glob.Glob, 0)

	err := t.setupFiles(b, ignore)
	if err != nil {
		return nil, err
	}

	err = t.walkFiles(b, ignore)
	if err != nil {
		return nil, err
	}

	for _, d := range t.Files {
		err = t.addFileFromRange(b, d)
		if err != nil {
			return nil, err
		}
	}

	return b, nil
}

func (t *Target) utilNewBuilderHandleVCFGAndIcon(b vpkg.Builder) error {

	err := t.handleVCFG(b)
	if err != nil {
		return err
	}

	err = t.handleIcon(b)
	if err != nil {
		return err
	}

	return nil
}

func (t *Target) handleVCFG(b vpkg.Builder) error {

	vcfg, err := t.VCFG()
	if err != nil {
		return err
	}

	x, err := vcfg.Marshal()
	if err != nil {
		return err
	}

	cfg := vio.CustomFile(vio.CustomFileArgs{
		Name:       "default.vcfg",
		ReadCloser: ioutil.NopCloser(bytes.NewReader(x)),
		Size:       len(x),
		ModTime:    time.Now(),
	})

	err = b.SetVCFG(cfg)
	if err != nil {
		return err
	}

	return nil
}

func (t *Target) handleIcon(b vpkg.Builder) error {

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
				return err
			}

			if icon.IsDir() {
				err = fmt.Errorf("icon file is a directory: %s", t.Icon)
				return err
			}
		}

		err := b.SetIcon(icon)
		if err != nil {
			return err
		}

	}
	return nil
}

func (t *Target) rangeIgnoreFiles(ignore []glob.Glob) error {

	for _, p := range t.Ignore {
		ip, err := glob.Compile(p)
		if err != nil {
			return err
		}

		ignore = append(ignore, ip)
	}

	return nil
}

func (t *Target) addFile(b vpkg.Builder, abs, path string) error {
	f, err := vio.LazyOpen(abs)
	if err != nil {
		return err
	}

	err = b.AddToFS(path, f)
	if err != nil {
		return err
	}

	return nil
}

func (t *Target) walkFiles(b vpkg.Builder, ignore []glob.Glob) error {
	return filepath.Walk(t.Dir, func(path string, info os.FileInfo, err error) error {

		path = filepath.ToSlash(path)
		abs := path
		path = strings.TrimPrefix(path, t.Dir)
		path = strings.TrimPrefix(path, "/")
		if path == "" || err != nil {
			return err
		}

		for _, ip := range ignore {
			if ip.Match(path) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		err = t.addFile(b, abs, path)
		if err != nil {
			return err
		}

		return nil
	})
}

func (t *Target) walkFunc(b vpkg.Builder, abs string) error {
	return filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {

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

		err = t.walkFuncHandleFile(b, f, abs, path, thisAbs)
		if err != nil {
			return err
		}

		return nil
	})
}

func (t *Target) walkFuncHandleFile(b vpkg.Builder, f vio.File, abs, path, thisAbs string) error {

	var err error
	if f.IsDir() {
		err = filepath.Walk(abs, func(path string, info os.FileInfo, err error) error {

			thisDir := path
			path = strings.TrimPrefix(path, abs)
			path = strings.TrimPrefix(path, "/")
			if path == "" {
				return nil
			}

			err = t.addFile(b, thisDir, path)
			return err
		})
	} else {
		err = b.AddToFS(path, f)
	}

	return err
}

func (t *Target) addFileFromRange(b vpkg.Builder, d string) error {

	abs := d
	if !filepath.IsAbs(d) {
		abs = filepath.Join(t.Dir, d)
	}

	abs = filepath.ToSlash(abs)

	err := t.walkFunc(b, abs)
	if err != nil {
		return err
	}

	return nil
}

func (t *Target) setupFiles(b vpkg.Builder, ignore []glob.Glob) error {
	err := t.utilNewBuilderHandleVCFGAndIcon(b)
	if err != nil {
		return err
	}

	err = t.rangeIgnoreFiles(ignore)
	if err != nil {
		return err
	}

	t.Dir, err = filepath.Abs(t.Dir)
	if err != nil {
		return err
	}
	t.Dir = filepath.ToSlash(t.Dir)

	return nil
}
