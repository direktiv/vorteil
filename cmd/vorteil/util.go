/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/vpkg"
)

func handleDirectory(src string, dst string, builder vpkg.Builder) error {
	// create subtree
	tree, err := vio.FileTreeFromDirectory(src)
	if err != nil {
		return err
	}

	x := strings.Split(strings.TrimSuffix(filepath.ToSlash(src), "/"), "/")
	err = builder.AddSubTreeToFS(filepath.Join(dst, x[len(x)-1]), tree)
	if err != nil {
		return err
	}
	return nil
}

func handleFile(src string, dst string, builder vpkg.Builder) error {
	// create file object
	f, err := vio.LazyOpen(src)
	if err != nil {
		return err
	}

	err = builder.AddToFS(filepath.Join(dst, filepath.Base(f.Name())), f)
	if err != nil {
		return err
	}
	return nil
}

func handleFileInjections(builder vpkg.Builder) error {
	for src, v := range filesMap {
		for _, dst := range v {
			fmt.Printf("SRC: %s\n", src)
			stat, err := os.Stat(src)
			if err != nil {
				return err
			}

			if stat.IsDir() {
				err = handleDirectory(src, dst, builder)
				if err != nil {
					return err
				}
			} else {
				err = handleFile(src, dst, builder)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}
