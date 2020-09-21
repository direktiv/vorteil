/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

package vproj

import (
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

var prefix = ""

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

	if err := isoOperation.initialize(); err != nil {
		return nil, err
	}

	return &isoOperation, nil
}

type importSharedObjectsOperation struct {
	projectDir string
	w          io.Writer

	count float64

	progressPaths       map[string]bool
	mapLock             sync.Mutex
	filesDone           map[string]interface{}
	unfoundDependencies map[string]interface{}
	foundAtLeast1File   bool

	excludeDefaultLibs bool
	imported32bit      bool
	imported64bit      bool

	ldPATHS []string

	logger elog.View
}

// initialize: Locate shared objects and save them to a map in the importSharedObjectsOperation object
func (isoOp *importSharedObjectsOperation) initialize() error {
	isoOp.progressPaths = make(map[string]bool)
	if err := isoOp.initLDPATHS(); err != nil {
		return fmt.Errorf("could not get all ld library paths, error: %v", err)
	}

	var recurseCount func(string) error
	recurseCount = func(d string) error {
		fs, err := ioutil.ReadDir(d)
		if err != nil {
			return err
		}
		for _, f := range fs {
			if f.IsDir() {
				return recurseCount(filepath.Join(d, f.Name()))
			}
			isoOp.progressPaths[filepath.Join(d, f.Name())] = true
		}
		return nil
	}

	err := recurseCount(isoOp.projectDir)
	if err != nil {
		return err
	}

	isoOp.count = float64(len(isoOp.progressPaths))

	// Init Map Files
	isoOp.mapLock = sync.Mutex{}
	isoOp.filesDone = make(map[string]interface{})
	isoOp.unfoundDependencies = make(map[string]interface{})

	return nil
}

//ReadLink for wsl symlink checking.
func ReadLink(path string) (string, error) {
	cmd := exec.Command("bash", "-c", fmt.Sprintf("readlink %s", path))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

//listDependencies: Given a file path 'fpath' locate the files dependencies.
//	Return a list of paths to dependencies found on system, and a list names of dependencies who are missing from system.
func (isoOp *importSharedObjectsOperation) listDependencies(fpath string) ([]string, []string, error) {
	e, err := elf.Open(fpath)
	if err != nil {
		if strings.HasPrefix(err.Error(), "bad magic number") {
			return []string{}, nil, nil
		}
		if errors.Is(err, io.EOF) {
			return []string{}, []string{}, nil
		}
		if strings.Contains(err.Error(), "The name of the file cannot be resolved by the system.") {
			// symlink reopen elf at point
			linuxPath := filepath.ToSlash(strings.TrimPrefix(fpath, prefix))
			target, err := ReadLink(linuxPath)
			if err != nil {
				return nil, nil, fmt.Errorf("unable to read link : %w", err)
			}
			if strings.HasPrefix(target, "/") {
				target = filepath.Join(prefix, target)
			} else {
				target = filepath.Join(prefix, filepath.Dir(linuxPath), target)
			}

			e, err = elf.Open(target)
			if err != nil {
				return nil, nil, fmt.Errorf("unable to scan file : %w", err)
			}
		} else {
			return nil, nil, fmt.Errorf("unable to scan file : %w", err)
		}
	}

	defer e.Close()

	if e.FileHeader.Class == elf.ELFCLASS32 {
		isoOp.imported32bit = true
	} else if e.FileHeader.Class == elf.ELFCLASS64 {
		isoOp.imported64bit = true
	}

	libs, err := e.ImportedLibraries()
	if err != nil {
		return nil, nil, fmt.Errorf("unable to load list of imported libraries from elf binary: %w", err)
	}
	var paths []string
	y := os.Getenv("LD_LIBRARY_PATH")
	if y != "" {
		x := filepath.SplitList(y)
		paths = append(paths, x...)
	}
	x, err := e.DynString(elf.DT_RPATH)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to load rpath from elf binary: %w", err)
	}
	paths = append(paths, x...)
	x, err = e.DynString(elf.DT_RUNPATH)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to load runpath from elf binary: %w", err)
	}
	paths = append(paths, x...)
	// system paths path (/etc/ld.so.conf)
	var loadPathsFromFile func(path string) error
	loadPathsFromFile = func(path string) error {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "/") {
				paths = append(paths, line)
				continue
			}
			if strings.HasPrefix(line, "include") {
				line = strings.TrimPrefix(line, "include ")
				line = strings.TrimSpace(line)
				line = filepath.ToSlash(filepath.Join(prefix, line))

				matches, err := filepath.Glob(line)
				if err != nil {
					return err
				}
				for _, match := range matches {
					err = loadPathsFromFile(match)
					if err != nil {
						return err
					}
				}
				continue
			}
			if strings.TrimSpace(line) != "" {
				if !strings.HasPrefix(line, "#") {
					return fmt.Errorf("unexpected line in '%s' file: %s", DynamicLinkerConfig, line)
				}
			}
		}

		return nil
	}

	err = loadPathsFromFile(filepath.Join(prefix, DynamicLinkerConfig))
	if err != nil {
		return nil, nil, err
	}
	paths = append(paths, filepath.Join(prefix, DefaultLinuxLibPath))
	paths = append(paths, filepath.Join(prefix, DefaultLinuxUserLibPath))

	fn := func(lib string) (string, error) {
		for _, sp := range paths {
			if strings.HasPrefix(sp, "$ORIGIN") {
				sp = strings.Replace(sp, "$ORIGIN", ".", 1)
				sp = filepath.Join(filepath.Dir(fpath), sp)
			}

			sp = filepath.Join(sp, lib)
			if runtime.GOOS == "windows" {
				if !strings.HasPrefix(sp, prefix) {
					sp = filepath.Join(prefix, sp)
				}
			}
			// prefix stat to get the elf open rather than returning
			_, err := os.Stat(sp)
			if err != nil {
				if !os.IsNotExist(err) {
					return "", fmt.Errorf("unable to stat candidate dependency: %w", err)
				}
				continue
			}

			l, err := elf.Open(sp)
			if err != nil {
				if strings.Contains(err.Error(), "The name of the file cannot be resolved by the system.") {
					// symlink reopen elf at point
					linuxPath := filepath.ToSlash(strings.TrimPrefix(sp, prefix))
					target, err := ReadLink(linuxPath)
					if err != nil {
						return "", errorDependencyReadlink(err)
					}
					if strings.HasPrefix(target, "/") {
						target = filepath.Join(prefix, target)
					} else {
						target = filepath.Join(prefix, filepath.Dir(linuxPath), target)
					}
					l, err = elf.Open(target)
					if err != nil {
						return "", errorDependencyScan(err)
					}
				} else {
					return "", errorDependencyScan(err)
				}
			}
			defer l.Close()

			if e.FileHeader.Class == l.FileHeader.Class {
				return sp, nil
			}
		}

		return "", fmt.Errorf("unable to locate dependency: %s", lib)
	}

	var deps []string
	var errs []error

	for _, p := range e.Progs {
		if p.ProgHeader.Type == elf.PT_INTERP {
			// this is the interpreter
			b, err := ioutil.ReadAll(p.Open())
			if err != nil {
				return nil, nil, err
			}

			var interpreterPath string
			for _, x := range b {
				if x == 0x00 {
					break
				}
				interpreterPath = fmt.Sprintf("%s%s", interpreterPath, string(x))
			}
			deps = append(deps, interpreterPath)
		}
	}

	noLocates := make([]string, 0)

	for _, lib := range libs {
		dep, lerr := fn(lib)
		if lerr != nil {
			locationErrPrefix := "unable to locate dependency: "
			if strings.HasPrefix(lerr.Error(), locationErrPrefix) {
				noLocates = append(noLocates, strings.TrimPrefix(lerr.Error(), locationErrPrefix))
			} else {
				errs = append(errs, lerr)
			}
		} else {
			deps = append(deps, dep)
		}
	}

	if len(errs) > 0 {
		if len(errs) == 1 {
			return deps, noLocates, errs[0]
		}
		var msgs []string
		for _, err := range errs {
			msgs = append(msgs, err.Error())
		}
		return deps, noLocates, fmt.Errorf("errors locating libs: %s", strings.Join(msgs, "; "))
	}

	return deps, noLocates, nil
}

//appendSliceToUnfoundDependencies appends missingDeps arg to operations unfoundDependencies
func (isoOp *importSharedObjectsOperation) appendSliceToUnfoundDependencies(missingDeps []string) () {
	for _, l := range missingDeps {
		isoOp.unfoundDependencies[l] = nil
	}
}

//initLDPATHS: initializes importSharedObjectsOperation.ldPATHS so that it may be used later with findLib func
func (isoOp *importSharedObjectsOperation) initLDPATHS() error {
	// Load paths from env
	isoOp.ldPATHS = getLDPathsFromENV()

	// Load paths from linker config
	if err := isoOp.loadLDPathsFromLinkerConfig(filepath.Join(prefix, DynamicLinkerConfig)); err != nil {
		return err
	}

	// Append Common Linux Lib Paths
	isoOp.ldPATHS = append(isoOp.ldPATHS, filepath.Join(prefix, DefaultLinuxLibPath))
	isoOp.ldPATHS = append(isoOp.ldPATHS, filepath.Join(prefix, DefaultLinuxUserLibPath))

	return nil
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
			line = strings.TrimPrefix(line, "include ")
			line = strings.TrimSpace(line)
			line = filepath.ToSlash(filepath.Join(prefix, line))
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

//findLib: Given a library name and class, locate that libraries path on the system and return the path.
func (isoOp *importSharedObjectsOperation) findLib(name string, class elf.Class) (string, error) {
	for _, sp := range isoOp.ldPATHS {
		sp = filepath.Join(sp, name)
		if runtime.GOOS == "windows" {
			if !strings.HasPrefix(sp, prefix) {
				sp = filepath.Join(prefix, sp)
			}
		}
		_, err := os.Stat(sp)
		if err != nil {
			if !os.IsNotExist(err) {
				return "", fmt.Errorf("unable to stat candidate dependency: %w", err)
			}
			continue
		}
		l, err := elf.Open(sp)
		if err != nil {
			if strings.Contains(err.Error(), "The name of the file cannot be resolved by the system.") {
				// symlink reopen elf at point
				linuxPath := filepath.ToSlash(strings.TrimPrefix(sp, prefix))
				target, err := ReadLink(linuxPath)
				if err != nil {
					return "", errorDependencyReadlink(err)
				}
				if strings.HasPrefix(target, "/") {
					target = filepath.Join(prefix, target)
				} else {
					target = filepath.Join(prefix, filepath.Dir(linuxPath), target)
				}

				l, err = elf.Open(target)
				if err != nil {
					return "", errorDependencyScan(err)
				}
			} else {
				return "", errorDependencyScan(err)
			}
		}
		defer l.Close()
		if class == l.FileHeader.Class {
			return sp, nil
		}
	}

	return "", fmt.Errorf("unable to locate dependency: %s", name)
}

//registerFile Add filePath to operations filesDone Map if it does not exist in keys.
//	If it does, return 'skip' set to true bool
func (isoOp *importSharedObjectsOperation) registerFile(filePath string) bool {
	isoOp.mapLock.Lock()
	var skip bool
	if _, ok := isoOp.filesDone[filePath]; !ok {
		// Path is not in map; register filePath in map
		isoOp.filesDone[filePath] = nil
	} else {
		// Path is already in map, set skip bool to true
		skip = true
	}
	isoOp.mapLock.Unlock()
	return skip
}

// Start: Start the process of scanning for shared objects and copying them into you project path.
//	If excludeDefaultLibs was set to false in builder, also copy default libs.
func (isoOp *importSharedObjectsOperation) Start() error {

	isoProgress := isoOp.logger.NewProgress("Importing Shared Objects ", "", 0)
	defer isoProgress.Finish(true)

	var recurseFile func(string) error
	recurseFile = func(path string) error {
		if skip := isoOp.registerFile(path); skip {
			return nil
		}

		if !strings.HasPrefix(path, isoOp.projectDir) {
			// file is outside of project projectDir -- copy it in and then check for lib dependencies
			isoOp.logger.Infof("copying '%s'", path)

			ls, err := os.Lstat(path)
			if err != nil {
				return err
			}
			adjustedPath := path
			if strings.HasPrefix(adjustedPath, DefaultLinuxUserLibPath) {
				if _, err = os.Stat(filepath.Join(isoOp.projectDir, adjustedPath)); err == nil {
					isoOp.logger.Warnf(fmt.Sprintf("Skipping '%s' -- file from higher priority source already exists within the project directory.", adjustedPath))
				}
				isoOp.logger.Warnf(fmt.Sprintf("Adjusted lib name (/usr/lib -> /lib) for: %s", path))
				adjustedPath = strings.TrimPrefix(adjustedPath, "/usr")
			}

			// if path points to a symlink
			if ls.Mode()&os.ModeSymlink == os.ModeSymlink {
				target, err := os.Readlink(path)
				if err != nil {
					return err
				}

				adjustedTarget := target
				if strings.HasPrefix(target, DefaultLinuxUserLibPath) {
					isoOp.logger.Warnf(fmt.Sprintf("Adjusted symlink target (/usr/lib -> /lib) for: %s", target))
					adjustedTarget = strings.TrimPrefix(adjustedTarget, "/usr")
				}

				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(path), target)
				}

				isoOp.logger.Debugf(fmt.Sprintf("found symlink: %s -> %s", path, target))

				err = recurseFile(target)
				if err != nil {
					return err
				}

				projectNewName := filepath.Join(isoOp.projectDir, adjustedPath)

				err = os.MkdirAll(filepath.Dir(projectNewName), 0777)
				if err != nil {
					return err
				}

				if stat, err := os.Lstat(projectNewName); err == nil {
					if stat.Mode()&os.ModeSymlink == os.ModeSymlink {
						err = os.Remove(projectNewName)
						if err != nil {
							return err
						}
					}
				}

				err = os.Symlink(adjustedTarget, projectNewName)
				if err != nil {
					return err
				}

			} else {
				newName := adjustedPath
				if !strings.HasPrefix(newName, isoOp.projectDir) {
					newName = filepath.Join(isoOp.projectDir, adjustedPath)
				}

				if _, err := os.Stat(newName); os.IsNotExist(err) {
					err := func() error {
						err := os.MkdirAll(filepath.Dir(newName), 0777)
						if err != nil {
							return err
						}

						f, err := os.Create(newName)
						if err != nil {
							return err
						}
						defer f.Close()

						src, err := os.Open(path)
						if err != nil {
							return err
						}
						defer src.Close()

						_, err = io.Copy(f, src)
						if err != nil {
							return err
						}
						return nil
					}()
					if err != nil {
						return err
					}
				}
			}
		}

		if strings.HasPrefix(path, DefaultLinuxLibPath) || strings.HasPrefix(path, "\\lib") {
			// append win prefix to find actual dependencies
			path = filepath.Join(prefix, path)
		}

		libs, missingLibs, err := isoOp.listDependencies(path)
		if err != nil {
			return err
		}

		isoOp.foundAtLeast1File = len(libs) > 0
		isoOp.appendSliceToUnfoundDependencies(missingLibs)

		for _, l := range libs {
			err = recurseFile(filepath.Join(prefix, l))
			if err != nil {
				return err
			}
		}

		return nil
	}

	errs := make([]error, 0)
	paths := make(chan string)
	endPathScan := "!!! END"

	var recursiveTreeSearch func(string) error
	recursiveTreeSearch = func(dir string) error {
		fis, err := ioutil.ReadDir(dir)
		if err != nil {
			return err
		}

		for _, fi := range fis {
			// Recursively search if fi is a directory
			if fi.Mode()&os.ModeSymlink == os.ModeSymlink {
				continue
			}

			if fi.IsDir() {
				err = recursiveTreeSearch(filepath.Join(dir, fi.Name()))
				if err != nil {
					return err
				}
			} else {
				// Get full filepath if fi is a file
				paths <- filepath.Join(dir, fi.Name())
			}

		}

		return nil
	}

	go func() {

		err := recursiveTreeSearch(isoOp.projectDir)
		if err != nil {
			errs = append(errs, err)
		}

		paths <- endPathScan

	}()

	for {
		select {
		case p := <-paths:
			if p == endPathScan {
				if !isoOp.excludeDefaultLibs && !isoOp.foundAtLeast1File {
					isoOp.logger.Warnf("omitting default libs --- no libs were required by the project.")
				}

				if !isoOp.excludeDefaultLibs && isoOp.foundAtLeast1File {
					isoOp.logger.Infof("copying default libs")

					defaultLibPaths := make([]string, 0)
					classes := []elf.Class{}

					if isoOp.imported32bit {
						classes = append(classes, elf.ELFCLASS32)
					} else {
						isoOp.logger.Infof("omitting 32-bit default libs --- no 32-bit binaries detected.")
					}

					if isoOp.imported64bit {
						classes = append(classes, elf.ELFCLASS64)
					} else {
						isoOp.logger.Infof("omitting 64-bit default libs --- no 64-bit binaries detected.")
					}

					for _, l := range DefaultLibs {
						for _, c := range classes {
							path, err := isoOp.findLib(l, c)
							if err != nil {
								isoOp.logger.Warnf(err.Error())
								continue
							}

							defaultLibPaths = append(defaultLibPaths, path)
						}
					}
					for _, p := range defaultLibPaths {
						err := recurseFile(p)
						if err != nil {
							return err
						}
					}
				}

				for l := range isoOp.unfoundDependencies {
					isoOp.logger.Warnf(fmt.Sprintf("unable to locate dependency: %s", l))
				}

				goto END
			}

			if err := recurseFile(p); err != nil {
				return err
			}
		}
	}

END:
	if len(isoOp.unfoundDependencies) != 0 {
		isoOp.logger.Printf("Completed with warnings.")
	} else {
		isoOp.logger.Printf("Completed.")
	}
	return nil
}

// ERRORS
func errorDependencyScan(err error) error {
	return fmt.Errorf("unable to scan candidate dependency: %w", err)
}

func errorDependencyReadlink(err error) error {
	return fmt.Errorf("unable to readlink dependency: %w", err)
}
