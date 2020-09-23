/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

package vproj

import (
	"debug/elf"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/vorteil/vorteil/pkg/elog"
)

const (
	DynamicLinkerConfig     = "/etc/ld.so.conf"
	WindowsWSLPrefix        = "\\\\wsl$\\Ubuntu-18.04"
	DefaultLinuxUserLibPath = "/usr/lib"
	DefaultLinuxLibPath     = "/lib"
)

var DefaultLibs = []string{"libnss_dns.so.2", "libnss_files.so.2", "libresolv.so.2"}

// NewImportSharedObject: This function is used to create and initialize a importSharedObjectsOperation.
// 	This function requires two args:
//			projectPath: The target project you wish to scan, and import shared objects to.
//			excludeDefaultLibs: Whether or not to exclude default libraries.
// 	Once initialized a importSharedObjectsOperation object will be returned.
// 	Running importSharedObjectsOperation.Start() will then begin the operation.
func NewImportSharedObject(projectPath string, excludeDefaultLibs bool, logger elog.View) (*importSharedObjectsOperation, error) {
	var isoOperation importSharedObjectsOperation

	isoOperation.projectDir = projectPath
	isoOperation.excludeDefaultLibs = excludeDefaultLibs
	isoOperation.logger = logger
	isoOperation.copiedSharedObjects = make(map[string]string)

	if err := isoOperation.initLDPATHS(); err != nil {
		return nil, err
	}

	return &isoOperation, nil
}

type importSharedObjectsOperation struct {
	projectDir string
	w          io.Writer

	count float64

	copiedSharedObjects map[string]string
	mapLock             sync.Mutex

	excludeDefaultLibs bool
	imported32bit      bool
	imported64bit      bool

	ldPATHS []string

	logger elog.View
}

type sharedObjectInfo struct {
	path    string
	elfType elf.Class
	found   bool
}

func getLDPathsFromENV() []string {
	var paths = make([]string, 0, 0)
	y := os.Getenv("LD_LIBRARY_PATH")
	if y != "" {
		x := filepath.SplitList(y)
		paths = append(paths, x...)
	}

	return paths
}

func (isoOp *importSharedObjectsOperation) loadLDPathsFromLinkerConfig(path string) error {
	if strings.Contains(path, "*") {
		return nil
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "/") {
			isoOp.ldPATHS = append(isoOp.ldPATHS, line)
			continue
		}

		if strings.HasPrefix(line, "include") {
			line = filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(line, "include ")))
			matches, err := filepath.Glob(line)
			if err != nil {
				return err
			}

			for _, match := range matches {
				err = isoOp.loadLDPathsFromLinkerConfig(match)
				if err != nil {
					return err
				}
			}
			err = isoOp.loadLDPathsFromLinkerConfig(line)
			if err != nil {
				return err
			}
		}
		if strings.TrimSpace(line) != "" {
			if strings.HasPrefix(line, "#") || strings.Contains(line, "*") {
				continue
			}
			return fmt.Errorf("unexpected line in '%s' file: %s", DynamicLinkerConfig, line)
		}
	}
	return nil
}

func (isoOp *importSharedObjectsOperation) initLDPATHS() error {
	// Load paths from env
	isoOp.ldPATHS = getLDPathsFromENV()
	// Load paths from linker config
	if err := isoOp.loadLDPathsFromLinkerConfig(DynamicLinkerConfig); err != nil {
		return err
	}
	// Append Common Linux Lib Paths
	isoOp.ldPATHS = append(isoOp.ldPATHS, DefaultLinuxLibPath)
	isoOp.ldPATHS = append(isoOp.ldPATHS, DefaultLinuxUserLibPath)
	return nil
}

func (isoOp *importSharedObjectsOperation) Start() error {
	var err error
	var projectPaths []string

	isoProgress := isoOp.logger.NewProgress("Importing Shared Objects ", "", 0)
	defer isoProgress.Finish(true)

	projectPaths, err = isoOp.getProjectFiles()
	if err != nil {
		goto ERROR
	}

	for i := range projectPaths {
		isoOp.addSharedObjects(projectPaths[i])
	}

	if !isoOp.excludeDefaultLibs {
		isoOp.logger.Infof("including Default Libs")
		for i := range DefaultLibs {
			elfLibPath, found, err := isoOp.findLib(DefaultLibs[i])
			if err == nil && found {
				isoOp.copiedSharedObjects[DefaultLibs[i]] = elfLibPath
				err := isoOp.addSharedObjects(elfLibPath)
				if err != nil {
					goto ERROR
				}
			} else if err == nil {
				isoOp.copiedSharedObjects[DefaultLibs[i]] = elfLibPath
			} else {
				goto ERROR
			}
		}
	}

	err = isoOp.copySharedObjects()
	if err != nil {
		goto ERROR
	}

	isoOp.logger.Printf("Completed.")
	return nil
ERROR:
	isoOp.logger.Errorf("Import shared Objects failed, error: %v", err)
	return nil
}

func (isoOp *importSharedObjectsOperation) copySharedObjects() error {
	for so, soPath := range isoOp.copiedSharedObjects {
		soProjectPath := filepath.Join(isoOp.projectDir, soPath)
		if soPath == "" {
			// Unfound Shared Object
			isoOp.logger.Warnf("shared object '%s' could not be found, so has been skipped", so)
		} else {
			if copied, err := isoOp.copyLib(soPath, soProjectPath); !copied {
				isoOp.logger.Debugf("skipping '%s' already exists", so)
			} else if err != nil {
				isoOp.logger.Errorf("failed to copy '%s'", so)
				return err
			}
		}
	}

	return nil
}

func (isoOp *importSharedObjectsOperation) copyLib(libPath, destPath string) (bool, error) {
	destPath = isoOp.adjustPath(destPath)
	isoOp.logger.Infof("copying '%s' > '%s", libPath, destPath)

	realLibPath, err := filepath.EvalSymlinks(libPath)
	if err != nil {
		panic(err)
	}

	// If target link has same name, inherit real path
	if filepath.Base(libPath) == filepath.Base(realLibPath) {
		libPath = realLibPath
	}

	// Check if path exists
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		err := os.MkdirAll(filepath.Dir(destPath), 0777)
		if err != nil {
			return false, err
		}

		// If true libPath is a symlink
		if realLibPath != libPath {
			isoOp.logger.Debugf("found symlink %s > %s", destPath, realLibPath)
			err := os.Symlink(strings.TrimPrefix(realLibPath, "/usr"), destPath)
			if err != nil {
				return false, err
			}
			return isoOp.copyLib(realLibPath, filepath.Join(isoOp.projectDir, realLibPath))
		} else {
			f, err := os.Create(destPath)
			if err != nil {
				return false, err
			}
			defer f.Close()

			src, err := os.Open(libPath)
			if err != nil {
				return false, err
			}
			defer src.Close()

			_, err = io.Copy(f, src)
			if err != nil {
				return false, err
			}
		}
	} else {
		return false, nil
	}

	return true, nil
}

func (isoOp *importSharedObjectsOperation) adjustPath(path string) string {
	adjustedPrefix := filepath.Join(isoOp.projectDir, "/usr")
	if strings.HasPrefix(path, adjustedPrefix) {
		path = strings.TrimPrefix(path, adjustedPrefix)
		path = filepath.Join(isoOp.projectDir, path)
	}

	if strings.HasPrefix(filepath.Base(path), "ld-linux-x86-64") {
		path = filepath.Join(isoOp.projectDir, "/lib64", filepath.Base(path))
	}

	return path
}

func (isoOp *importSharedObjectsOperation) addSharedObjects(fPath string) error {
	elfFile, err := elf.Open(fPath)
	if err != nil {
		return err
	}
	defer elfFile.Close()
	elfLibs, err := elfFile.ImportedLibraries()
	if err != nil {
		return err
	}

	isoOp.setValidClass(elfFile.FileHeader.Class)

	for i := range elfLibs {
		if _, ok := isoOp.copiedSharedObjects[elfLibs[i]]; !ok {
			elfLibPath, found, err := isoOp.findLib(elfLibs[i])
			if err == nil && found {
				isoOp.copiedSharedObjects[elfLibs[i]] = elfLibPath
				if err := isoOp.addSharedObjects(elfLibPath); err != nil {
					return err
				}
			} else if err == nil {
				isoOp.copiedSharedObjects[elfLibs[i]] = elfLibPath
			} else {
				return err
			}
		}
	}

	return nil
}

func (isoOp *importSharedObjectsOperation) findLib(libName string) (string, bool, error) {
	for _, ldPath := range isoOp.ldPATHS {
		potentialPath := filepath.Join(ldPath, libName)

		// Check if potentialPath can be stat'd
		if _, err := os.Stat(potentialPath); err != nil && !os.IsNotExist(err) {
			return "", false, fmt.Errorf("unable to stat candidate dependency: %w", err)
		} else if err != nil {
			continue
		}

		l, err := elf.Open(potentialPath)
		if err != nil {
			return "", false, errorDependencyScan(err)
		}

		if isoOp.isValidClass(l.FileHeader.Class) {
			isoOp.logger.Debugf("Found Path for library: %s at %s", libName, potentialPath)
			return potentialPath, true, nil
		}
	}

	isoOp.logger.Debugf("Could not find Path for library: %s", libName)
	// Unable to find lib
	return "", false, nil
}

func (isoOp *importSharedObjectsOperation) isValidClass(libClass elf.Class) bool {
	if isoOp.imported32bit && libClass == elf.ELFCLASS32 {
		return true
	}

	if isoOp.imported64bit && libClass == elf.ELFCLASS64 {
		return true
	}

	return false
}

func (isoOp *importSharedObjectsOperation) setValidClass(libClass elf.Class) {
	if !isoOp.imported32bit && libClass == elf.ELFCLASS32 {
		isoOp.imported32bit = true
	}

	if !isoOp.imported64bit && libClass == elf.ELFCLASS64 {
		isoOp.imported64bit = true
	}
}

// getListOfElfPath: will scan projectDir and return a list of paths that consists of every file in the project directory
func (isoOp *importSharedObjectsOperation) getProjectFiles() ([]string, error) {
	var projectPaths = make([]string, 0)
	err := filepath.Walk(isoOp.projectDir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				projectPaths = append(projectPaths, path)
			}
			return nil
		})

	if err != nil {
		return nil, err
	}

	return projectPaths, nil
}

// ERRORS
func errorDependencyScan(err error) error {
	return fmt.Errorf("unable to scan candidate dependency: %w", err)
}
