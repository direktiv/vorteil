/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */
package vconvert

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	log "github.com/sirupsen/logrus"
)

var (
	defaultProjectFile = `ignore = [".vorteilproject"]
[[target]]
  name = "default"
  vcfgs = ["default.vcfg"]`

	// ignore folders
	folders = []string{"dev", "proc", "sys", "boot", "media", "mnt", "tmp"}
)

// write function to write out tar layers
func writeFile(name string, r io.Reader) error {

	buf := make([]byte, 32768)

	f, err := os.Create(name)
	if err != nil {
		return err
	}
	for {
		n, err := r.Read(buf)
		if err != nil && err != io.EOF {
			return err
		}
		if n == 0 {
			break
		}

		if _, err := f.Write(buf[:n]); err != nil {
			return err
		}
	}

	return nil
}

// findBinary tries to find the executable in the expanded container image
func findBinary(name string, env []string, cwd string, targetDir string) (string, error) {

	if strings.HasPrefix(name, "./") {
		abs, err := filepath.Abs(name)
		if err != nil {
			log.Printf("can not get absolute path for %s: %s", name, err.Error())
			return name, nil
		}
		cwd, err := os.Getwd()
		if err != nil {
			log.Printf("can not get current working dir %s: %s", name, err.Error())
			return name, nil
		}
		rel, err := filepath.Rel(cwd, abs)
		if err != nil {
			log.Printf("can not get relative path for %s: %s", name, err.Error())
			return name, nil
		}
		name = rel
	}

	// absolute
	if strings.HasPrefix(name, "/") {
		fp := filepath.Join(targetDir, name)
		if _, err := os.Stat(fp); err == nil {
			return name, nil
		}
		return "", fmt.Errorf("can not find binary %s", name)
	}

	for _, e := range env {
		elems := strings.SplitN(e, "=", 2)
		if elems[0] == "PATH" {
			elems = strings.Split(elems[1], ":")
			for _, p := range elems {
				path := filepath.Join(targetDir, p, strings.ReplaceAll(name, "\"", ""))
				if _, err := os.Stat(path); err == nil {
					return filepath.Join(p, strings.ReplaceAll(name, "\"", "")), nil
				}
			}
		}
	}

	path := filepath.Join(targetDir, cwd, strings.ReplaceAll(name, "\"", ""))
	if _, err := os.Stat(path); err == nil {
		return filepath.Join(cwd, strings.ReplaceAll(name, "\"", "")), nil
	}

	return "", fmt.Errorf("can not find binary %s", name)
}

func checkDirectory(targetDir string) error {

	// check if it exists and empty
	if _, err := os.Stat(targetDir); err != nil {
		os.MkdirAll(targetDir, 0755)
	}

	fi, err := ioutil.ReadDir(targetDir)
	if err != nil {
		return err
	}
	if len(fi) > 0 {
		return fmt.Errorf("target directory %s not empty", targetDir)
	}

	return nil

}
