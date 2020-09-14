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
)

type ImportSharedObjectsOptions struct {
	ExcludeDefaultLibs bool

	LoggerWarn func(format string, a ...interface{})
	LoggerInfo func(format string, a ...interface{})
	LoggerDebug func(format string, a ...interface{})
}

func NewImportSharedObject(projectPath string, options ImportSharedObjectsOptions) (*importSharedObjectsOperation, error) {
	var isoOperation importSharedObjectsOperation

	isoOperation.projectDir = projectPath
	isoOperation.excludeDefaultLibs = options.ExcludeDefaultLibs

	// Set Loggers
	if options.LoggerInfo != nil {
		isoOperation.loggerInfo = options.LoggerInfo
	} else {
		isoOperation.loggerInfo = func(format string, a ...interface{}) {}
	}

	if options.LoggerWarn != nil {
		isoOperation.loggerWarn = options.LoggerWarn
	} else {
		isoOperation.loggerWarn = func(format string, a ...interface{}) {}
	}

	if options.LoggerDebug != nil {
		isoOperation.loggerDebug = options.LoggerDebug
	} else {
		isoOperation.loggerDebug = func(format string, a ...interface{}) {}
	}

	if err := isoOperation.initialize(); err != nil {
		return nil, err
	}

	return &isoOperation, nil
}

type importSharedObjectsOperation struct {
	projectDir string
	w          io.Writer

	progressPaths map[string]bool

	excludeDefaultLibs bool
	imported32bit      bool
	imported64bit      bool

	loggerWarn func(format string, a ...interface{})
	loggerInfo func(format string, a ...interface{})
	loggerDebug func(format string, a ...interface{})
}

func (isoOp *importSharedObjectsOperation) initialize() error {

	isoOp.progressPaths = make(map[string]bool)

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

func (isoOp *importSharedObjectsOperation) listDependencies(fpath string) ([]string, []string, error) {
	prefix := ""
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
					return fmt.Errorf("unexpected line in '/etc/ld.so.conf' file: %s", line)
				}
			}
		}

		return nil
	}

	err = loadPathsFromFile(filepath.Join(prefix, "/etc/ld.so.conf"))
	if err != nil {
		return nil, nil, err
	}
	paths = append(paths, filepath.Join(prefix, "/lib"))
	paths = append(paths, filepath.Join(prefix, "/usr/lib"))

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
						return "", fmt.Errorf("unable to readlink dependency: %w", err)
					}
					if strings.HasPrefix(target, "/") {
						target = filepath.Join(prefix, target)
					} else {
						target = filepath.Join(prefix, filepath.Dir(linuxPath), target)
					}
					l, err = elf.Open(target)
					if err != nil {
						return "", fmt.Errorf("unable to scan candidate dependency: %w", err)
					}
				} else {
					return "", fmt.Errorf("unable to scan candidate dependency: %w", err)
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

func findLib(name string, class elf.Class) (string, error) {
	prefix := ""
	var paths []string
	y := os.Getenv("LD_LIBRARY_PATH")
	if y != "" {
		x := filepath.SplitList(y)
		paths = append(paths, x...)
	}
	// system paths path (/etc/ld.so.conf)
	var loadPathsFromFile func(path string) error
	loadPathsFromFile = func(path string) error {
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
				err = loadPathsFromFile(line)
				if err != nil {
					return err
				}
			}
			if strings.TrimSpace(line) != "" {
				if strings.HasPrefix(line, "#") || strings.Contains(line, "*") {
					continue
				}
				return fmt.Errorf("unexpected line in '/etc/ld.so.conf' file: %s", line)
			}
		}

		return nil
	}

	err := loadPathsFromFile(filepath.Join(prefix, "/etc/ld.so.conf"))
	if err != nil {
		return "", err
	}
	paths = append(paths, filepath.Join(prefix, "/lib"))
	paths = append(paths, filepath.Join(prefix, "/usr/lib"))

	fn := func(lib string) (string, error) {
		for _, sp := range paths {
			sp = filepath.Join(sp, lib)
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
						return "", fmt.Errorf("unable to readlink dependency: %w", err)
					}
					if strings.HasPrefix(target, "/") {
						target = filepath.Join(prefix, target)
					} else {
						target = filepath.Join(prefix, filepath.Dir(linuxPath), target)
					}

					l, err = elf.Open(target)
					if err != nil {
						return "", fmt.Errorf("unable to scan candidate dependency: %w", err)
					}
				} else {
					return "", fmt.Errorf("unable to scan candidate dependency: %w", err)
				}
			}
			defer l.Close()
			if class == l.FileHeader.Class {
				return sp, nil
			}
		}

		return "", fmt.Errorf("unable to locate dependency: %s", lib)
	}

	return fn(name)
}

func (isoOp *importSharedObjectsOperation) Start() error {
	prefix := ""
	if runtime.GOOS == "windows" {
		prefix = "\\\\wsl$\\Ubuntu-18.04"
	}
	mapLock := sync.Mutex{}
	filesDone := make(map[string]interface{})

	unfoundDependencies := make(map[string]interface{})
	var foundAtLeast1File bool

	var recurseFile func(string) error
	recurseFile = func(path string) error {
		var skip bool
		mapLock.Lock()
		if _, ok := filesDone[path]; !ok {
			filesDone[path] = nil
		} else {
			skip = true
		}
		mapLock.Unlock()

		if skip {
			return nil
		}

		// } else {
		if !strings.HasPrefix(path, isoOp.projectDir) {
			// file is outside of project projectDir -- copy it in and then check for lib dependencies
			isoOp.loggerInfo("copying '%s'", path)

			ls, err := os.Lstat(path)
			if err != nil {
				return err
			}
			adjustedPath := path
			if strings.HasPrefix(adjustedPath, "/usr/lib") {
				if _, err = os.Stat(filepath.Join(isoOp.projectDir, adjustedPath)); err == nil {
					isoOp.loggerWarn(fmt.Sprintf("Skipping '%s' -- file from higher priority source already exists within the project directory.", adjustedPath))
				}
				isoOp.loggerWarn(fmt.Sprintf("Adjusted lib name (/usr/lib -> /lib) for: %s", path))
				adjustedPath = strings.TrimPrefix(adjustedPath, "/usr")
			}

			// if path points to a symlink
			if ls.Mode()&os.ModeSymlink == os.ModeSymlink {
				target, err := os.Readlink(path)
				if err != nil {
					return err
				}

				adjustedTarget := target
				if strings.HasPrefix(target, "/usr/lib") {
					isoOp.loggerWarn(fmt.Sprintf("Adjusted symlink target (/usr/lib -> /lib) for: %s", target))
					adjustedTarget = strings.TrimPrefix(adjustedTarget, "/usr")
				}

				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(path), target)
				}

				isoOp.loggerDebug(fmt.Sprintf("found symlink: %s -> %s", path, target))

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
		// }

		if strings.HasPrefix(path, "/lib") || strings.HasPrefix(path, "\\lib") {
			// append win prefix to find actual dependencies
			path = filepath.Join(prefix, path)
		}

		libs, missingLibs, err := isoOp.listDependencies(path)
		if err != nil {
			return err
		}
		if len(libs) > 0 {
			foundAtLeast1File = true
		}
		for _, l := range missingLibs {
			unfoundDependencies[l] = nil
		}

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
				if !isoOp.excludeDefaultLibs && !foundAtLeast1File {
					isoOp.loggerWarn("Omitting default libs --- no libs were required by the project.")
				}

				if !isoOp.excludeDefaultLibs && foundAtLeast1File {
					defaultLibs := []string{"libnss_dns.so.2", "libnss_files.so.2", "libresolv.so.2"}
					defaultLibPaths := make([]string, 0)
					classes := []elf.Class{}

					if isoOp.imported32bit {
						classes = append(classes, elf.ELFCLASS32)
					} else {
						isoOp.loggerInfo("Omitting 32-bit default libs --- no 32-bit binaries detected.")
					}

					if isoOp.imported64bit {
						classes = append(classes, elf.ELFCLASS64)
					} else {
						isoOp.loggerInfo("Omitting 64-bit default libs --- no 64-bit binaries detected.")
					}

					for _, l := range defaultLibs {
						for _, c := range classes {
							path, err := findLib(l, c)
							if err != nil {
								isoOp.loggerWarn(err.Error())
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

				for l, _ := range unfoundDependencies {
					isoOp.loggerWarn(fmt.Sprintf("unable to locate dependency: %s", l))
				}

				goto END
			}

			if err := recurseFile(p); err != nil {
				return err
			}
		}
	}

END:
	if len(unfoundDependencies) != 0 {
		isoOp.loggerInfo("Completed with warnings.")
	} else {
		isoOp.loggerInfo("Completed.")
	}

	return nil
}
