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

	"github.com/vorteil/vorteil/pkg/elog"
)

const (
	DynamicLinkerConfig     = "/etc/ld.so.conf"
	DefaultLinuxUserLibPath = "/usr/lib"
	DefaultLinuxLibPath     = "/lib"
)

var DefaultLibs = []string{"libnss_dns.so.2", "libnss_files.so.2", "libresolv.so.2"}

// NewImportSharedObject: This function is used to create and initialize a importSharedObjectsOperation.
// 	This function requires three args:
//			projectPath: The target project you wish to scan, and import shared objects to.
//			excludeDefaultLibs: Whether or not to exclude default libraries.
//			logger: logger object to log with
// 	Once initialized a importSharedObjectsOperation object will be returned.
// 	Running importSharedObjectsOperation.Start() will then begin the operation.
func NewImportSharedObject(projectPath string, excludeDefaultLibs bool, logger elog.View) (*importSharedObjectsOperation, error) {
	var isoOperation importSharedObjectsOperation

	isoOperation.projectDir = projectPath
	isoOperation.excludeDefaultLibs = excludeDefaultLibs
	isoOperation.logger = logger
	isoOperation.sharedObjects = make(map[string]string)

	if err := isoOperation.initLDPATHS(); err != nil {
		return nil, err
	}

	return &isoOperation, nil
}

type importSharedObjectsOperation struct {
	projectDir string

	sharedObjects map[string]string // Tracks shared objects, if value is "", shared object is missing from system

	excludeDefaultLibs  bool
	sharedObjectClasses []elf.Class
	imported32bit       bool
	imported64bit       bool

	ldPATHS []string // SYSTEM LD PATHS

	logger elog.View
}

//getLDPathsFromENV: Gets LD_LIBRARY_PATH env value and split the paths into a string slice
func getLDPathsFromENV() []string {
	var paths = make([]string, 0, 0)
	y := os.Getenv("LD_LIBRARY_PATH")
	if y != "" {
		x := filepath.SplitList(y)
		paths = append(paths, x...)
	}

	return paths
}

//loadLDPathsFromLinkerConfig Load LD config file located at parameter 1 'path', scan for library paths
//	and append these paths to importSharedObjectsOperation tracked paths at 'ldPATHS.
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

//initLDPATHS: initializes importSharedObjectsOperation.ldPATHS so that it may be used later with findLib func
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
	classesImported := make([]elf.Class, 0)

	isoProgress := isoOp.logger.NewProgress("Importing Shared Objects ", "", 0)
	defer isoProgress.Finish(true)

	// Get all a path to all files in project dir
	projectPaths, err = isoOp.getProjectFiles()
	if err != nil {
		goto ERROR
	}

	// Find Import Libraries of project files and add them to map
	for i := range projectPaths {
		isoOp.addSharedObjects(projectPaths[i])
	}

	// Find Import Libraries of default files and add them to map
	if isoOp.imported32bit {
		classesImported = append(classesImported, elf.ELFCLASS32)
	}
	if isoOp.imported64bit {
		classesImported = append(classesImported, elf.ELFCLASS64)
	}
	if !isoOp.excludeDefaultLibs {
		isoOp.logger.Infof("including Default Libs")
		for i := range classesImported {
			for j := range DefaultLibs {
				elfLibPath, found, err := isoOp.findLib(DefaultLibs[j], classesImported[i])
				if err == nil && found {
					isoOp.sharedObjects[DefaultLibs[j]] = elfLibPath
					err := isoOp.addSharedObjects(elfLibPath)
					if err != nil {
						goto ERROR
					}
				} else if err == nil {
					isoOp.sharedObjects[DefaultLibs[j]] = elfLibPath
				} else {
					goto ERROR
				}
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

// copySharedObjects: loops over stored sharedObjects in operations map and copies those
//	shared objects into project director
func (isoOp *importSharedObjectsOperation) copySharedObjects() error {
	for so, soPath := range isoOp.sharedObjects {
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

// copyLib: Copies file at libPath to destPath if it does not exists.
//	If destPath parent dir does not exists it is created.
//	If libPath is a symlink, evaluate that symlink and create a symlink to target at destPath
func (isoOp *importSharedObjectsOperation) copyLib(libPath, destPath string) (bool, error) {
	destPath = isoOp.adjustPath(destPath)
	isoOp.logger.Infof("copying '%s' > '%s", libPath, destPath)

	// evaluate libPath so see if its a symlink
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

			// Recall copyLib to copy symlinks target into projectDir
			return isoOp.copyLib(realLibPath, filepath.Join(isoOp.projectDir, realLibPath))
		} else {
			// libPath is not a symlink, Copy file over
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

//adjustPath: If /usr is the top level directory of the path, trim it out.
//	Readjust path of linker to /lib64
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

//addSharedObjects: Attempt to open path as an elf file, and recurisely walk through all of that files imported libraries
//	If a untracked imported libraries is found, add it to the sharedObjects map with the path as its value
func (isoOp *importSharedObjectsOperation) addSharedObjects(fPath string) error {
	elfFile, err := elf.Open(fPath)
	if err != nil {
		return err // File is not a valid elf
	}
	defer elfFile.Close()
	elfLibs, err := elfFile.ImportedLibraries()
	if err != nil {
		return err // Could not open Imported Libraries
	}

	isoOp.setValidClass(elfFile.FileHeader.Class)

	for i := range elfLibs {
		if _, ok := isoOp.sharedObjects[elfLibs[i]]; !ok {
			elfLibPath, found, err := isoOp.findLib(elfLibs[i], elfFile.FileHeader.Class)
			if err == nil && found {
				// Library path found, add to map and then search library for its own imported libraries
				isoOp.sharedObjects[elfLibs[i]] = elfLibPath
				if err := isoOp.addSharedObjects(elfLibPath); err != nil {
					return err
				}
			} else if err == nil {
				// Library path not found, add to map
				isoOp.sharedObjects[elfLibs[i]] = elfLibPath
			} else {
				return err
			}
		}
	}

	return nil
}

// Find the path of a library given the name and its elf class
func (isoOp *importSharedObjectsOperation) findLib(libName string, class elf.Class) (string, bool, error) {
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

		if l.FileHeader.Class == class {
			isoOp.logger.Debugf("Found Path for library: %s at %s", libName, potentialPath)
			return potentialPath, true, nil
		}
	}

	isoOp.logger.Debugf("Could not find Path for library: %s", libName)
	// Unable to find lib
	return "", false, nil
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
