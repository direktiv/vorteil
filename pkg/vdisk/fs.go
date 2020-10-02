package vdisk

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"
	"sort"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/ext"
	"github.com/vorteil/vorteil/pkg/vimg"
	"github.com/vorteil/vorteil/pkg/vio"
)

func init() {

	if len(FilesystemCompilers()) != 0 {
		return
	}

	fn := func(log elog.Logger, tree vio.FileTree, args interface{}) (vimg.FSCompiler, error) {
		return ext.NewCompiler(&ext.CompilerArgs{
			Logger:   log,
			FileTree: tree,
		}), nil
	}

	err := RegisterFilesystemCompiler("", fn)
	if err != nil {
		panic(err)
	}

	err = RegisterFilesystemCompiler("ext", fn)
	if err != nil {
		panic(err)
	}

	err = RegisterFilesystemCompiler("ext2", fn)
	if err != nil {
		panic(err)
	}

}

// FSCompilerInstantiator is a function that returns a new file-system compiler
// when provided with common arguments (any uncommon arguments can be passed
// through 'args').
type FSCompilerInstantiator func(log elog.Logger, tree vio.FileTree, args interface{}) (vimg.FSCompiler, error)

var registeredFSCompilers map[string]FSCompilerInstantiator

// RegisterFilesystemCompiler registers a FSCompilerInstantiator with a given
// name.
func RegisterFilesystemCompiler(name string, fn FSCompilerInstantiator) error {

	if registeredFSCompilers == nil {
		registeredFSCompilers = make(map[string]FSCompilerInstantiator)
	}

	if _, exists := registeredFSCompilers[name]; exists {
		return fmt.Errorf("refusing to register file-system compiler '%s': already registered", name)
	}

	registeredFSCompilers[name] = fn
	return nil

}

// FilesystemCompilers returns an alphabetised list of all registered
// file-system compilers. Note that a single compiler may appear multiple times
// in the list under different names.
func FilesystemCompilers() []string {

	var names = []string{}

	if registeredFSCompilers == nil {
		return names
	}

	for k := range registeredFSCompilers {
		names = append(names, k)
	}

	sort.Strings(names)
	return names

}

// DeregisterFilesystemCompiler deregisters a FSCompilerInstantiator for the
// given name.
func DeregisterFilesystemCompiler(name string) error {

	if registeredFSCompilers != nil {
		if _, exists := registeredFSCompilers[name]; exists {
			delete(registeredFSCompilers, name)
			return nil
		}
	}

	return fmt.Errorf("file-system compiler '%s' not found", name)

}

// NewFilesystemCompiler returns a vimg.FSCompiler object that can be used to
// start a Vorteil image build, if the named file-system compiler has been
// registered.
func NewFilesystemCompiler(name string, log elog.Logger, tree vio.FileTree, args interface{}) (vimg.FSCompiler, error) {

	fn, exists := registeredFSCompilers[name]
	if !exists {
		return nil, fmt.Errorf("file-system compiler '%s' not found", name)
	}

	return fn(log, tree, args)

}
