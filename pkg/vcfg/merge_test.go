package vcfg

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

var testLogging = Logging{
	Config: []string{"test1", "test2", "test3"},
	Type:   "test",
}

var replacementLogging = Logging{
	Type:   "test2",
	Config: []string{"test4"},
}

var genericNIC = NetworkInterface{
	IP:    "dhcp",
	UDP:   []string{"80", "81"},
	TCP:   []string{"82", "83"},
	HTTP:  []string{"84", "85"},
	HTTPS: []string{"86", "87"},
}

var replacementNIC = NetworkInterface{
	IP:      "192.168.0.1",
	UDP:     []string{"!80"},
	HTTP:    []string{"90", "91"},
	MTU:     128,
	TCPDUMP: true,
}

var genericNFS = NFSSettings{
	MountPoint: "/test",
	Server:     "192.168.0.1",
	Arguments:  "testarg",
}

var replacementNFS = NFSSettings{
	MountPoint: "/test2",
	Server:     "0.0.0.0",
	Arguments:  "",
}

var genericProgram = Program{
	Binary:    "HelloWorld",
	Bootstrap: []string{"SLEEP 3000"},
}

var replacementProgram = Program{
	Args:      "test1 test2 test3",
	Cwd:       "/",
	Strace:    true,
	LogFiles:  []string{"lf1"},
	Bootstrap: []string{"WAIT_FILE test3"},
	Privilege: "superuser",
	Stdout:    "stdout",
	Stderr:    "stderr",
}

var genericRoute = Route{
	Interface:   "test",
	Destination: "dest",
	Gateway:     "blah",
}

var replacementRoute = Route{
	Interface: "1",
	Gateway:   "3",
}

func TestMergeLogging(t *testing.T) {

	a := new(VCFG)
	b := new(VCFG)

	a.Logging = make([]Logging, 0)
	b.Logging = make([]Logging, 0)

	a.Logging = append(a.Logging, testLogging, testLogging)
	b.Logging = append(b.Logging, Logging{}, replacementLogging)

	err := a.mergeLogging(b)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(a.Logging))
	assert.Equal(t, testLogging, a.Logging[0])
	assert.Equal(t, append(testLogging.Config, "test4"), a.Logging[1].Config)
}

func TestMergeNetworks(t *testing.T) {

	a := new(VCFG)
	b := new(VCFG)

	a.Networks = make([]NetworkInterface, 0)
	b.Networks = make([]NetworkInterface, 0)

	a.Networks = append(a.Networks, genericNIC)
	b.Networks = append(b.Networks, replacementNIC, genericNIC)

	err := a.mergeNetworks(b)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(a.Networks))
	assert.Equal(t, genericNIC, a.Networks[1])
	assert.Equal(t, "192.168.0.1", a.Networks[0].IP)
	assert.Equal(t, uint(128), a.Networks[0].MTU)
	assert.Equal(t, []string{"80", "81", "!80"}, a.Networks[0].UDP) // !80 should still be in the array by this stage
	assert.Equal(t, genericNIC.TCP, a.Networks[0].TCP)
	assert.Equal(t, []string{"84", "85", "90", "91"}, a.Networks[0].HTTP)
	assert.Equal(t, true, a.Networks[0].TCPDUMP)

}

func TestMergeNFS(t *testing.T) {

	a := new(VCFG)
	b := new(VCFG)

	a.NFS = make([]NFSSettings, 0)
	b.NFS = make([]NFSSettings, 0)

	a.NFS = append(a.NFS, genericNFS, genericNFS)
	b.NFS = append(b.NFS, replacementNFS)

	err := a.mergeNFS(b)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(a.NFS))
	assert.Equal(t, genericNFS, a.NFS[1])
	assert.Equal(t, replacementNFS.MountPoint, a.NFS[0].MountPoint)
	assert.Equal(t, replacementNFS.Server, a.NFS[0].Server)
	assert.Equal(t, genericNFS.Arguments, a.NFS[0].Arguments)

}

func TestMergePrograms(t *testing.T) {

	a := new(VCFG)
	b := new(VCFG)

	a.Programs = make([]Program, 0)
	b.Programs = make([]Program, 0)

	a.Programs = append(a.Programs, genericProgram)
	b.Programs = append(b.Programs, replacementProgram, genericProgram, genericProgram)

	err := a.mergePrograms(b)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(a.Programs))
	assert.Equal(t, genericProgram, a.Programs[1])
	assert.Equal(t, genericProgram, a.Programs[2])
	assert.Equal(t, genericProgram.Binary, a.Programs[0].Binary)
	assert.Equal(t, append(genericProgram.Bootstrap, replacementProgram.Bootstrap[0]), a.Programs[0].Bootstrap)
	assert.Equal(t, replacementProgram.Privilege, a.Programs[0].Privilege)
	assert.Equal(t, replacementProgram.Stdout, a.Programs[0].Stdout)
	assert.Equal(t, replacementProgram.Stderr, a.Programs[0].Stderr)
	assert.Equal(t, replacementProgram.LogFiles, a.Programs[0].LogFiles)
	assert.Equal(t, replacementProgram.Strace, a.Programs[0].Strace)
	assert.Equal(t, replacementProgram.Cwd, a.Programs[0].Cwd)
	assert.Equal(t, replacementProgram.Args, a.Programs[0].Args)

}

func TestMergeRoutes(t *testing.T) {

	a := new(VCFG)
	b := new(VCFG)

	a.Routing = make([]Route, 0)
	b.Routing = make([]Route, 0)

	a.Routing = append(a.Routing, genericRoute)
	b.Routing = append(b.Routing, replacementRoute)

	err := a.mergeRoutes(b)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(a.Routing))
	assert.Equal(t, b.Routing[0].Interface, a.Routing[0].Interface)
	assert.Equal(t, genericRoute.Destination, a.Routing[0].Destination)
	assert.Equal(t, b.Routing[0].Gateway, a.Routing[0].Gateway)

}

func TestMerge(t *testing.T) {

	a := new(VCFG)
	b := new(VCFG)

	a.Programs = make([]Program, 0)
	a.Programs = append(a.Programs, genericProgram)
	a.NFS = make([]NFSSettings, 0)
	a.NFS = append(a.NFS, genericNFS)
	a.Routing = make([]Route, 0)
	a.Routing = append(a.Routing, genericRoute)
	a.Networks = make([]NetworkInterface, 0)
	a.Networks = append(a.Networks, genericNIC)
	a.Logging = make([]Logging, 0)
	a.Logging = append(a.Logging, testLogging)

	b.Programs = make([]Program, 0)
	b.Programs = append(b.Programs, replacementProgram)
	b.NFS = make([]NFSSettings, 0)
	b.NFS = append(b.NFS, replacementNFS)
	b.Routing = make([]Route, 0)
	b.Routing = append(b.Routing, replacementRoute)
	b.Networks = make([]NetworkInterface, 0)
	b.Networks = append(b.Networks, replacementNIC)
	b.Logging = make([]Logging, 0)
	b.Logging = append(b.Logging, replacementLogging)

	err := a.Merge(b)
	assert.NoError(t, err)

}
