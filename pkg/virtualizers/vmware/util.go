// Copyright 2016 Vorteil.io Pty Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package vmware

import (
	"runtime"
	"strings"

	"github.com/vorteil/vorteil/pkg/virtualizers"
)

const vmxFile = `#!/usr/bin/vmware
debugStub.listen.guest64 = "TRUE"
debugStub.hideBreakpoints= "TRUE"
debugStub.listen.guest64.remote = "TRUE"
.encoding = "UTF-8"
config.version = "8"
bios.bootdelay = "0"
virtualHW.version = "11"
vcpu.hotadd = "FALSE"
sata0.present = "FALSE"
memsize = "(MEM)"
mem.hotadd = "FALSE"
scsi0.present = "TRUE"
scsi0.virtualDev = "pvscsi"
scsi0:0.present = "TRUE"
scsi0:0.fileName = "(DISK)"

ethernet0.present = "(ETH0)"
ethernet0.connectionType = "(NETTYPE)"
ethernet0.virtualDev = "vmxnet3"
ethernet0.wakeOnPcktRcv = "FALSE"
ethernet0.addressType = "generated"
ethernet0.pciSlotNumber = "1024"
ethernet1.present = "(ETH1)"
ethernet1.connectionType = "(NETTYPE)"
ethernet1.virtualDev = "vmxnet3"
ethernet1.wakeOnPcktRcv = "FALSE"
ethernet1.addressType = "generated"
ethernet1.pciSlotNumber = "1025"
ethernet2.present = "(ETH2)"
ethernet2.connectionType = "(NETTYPE)"
ethernet2.virtualDev = "vmxnet3"
ethernet2.wakeOnPcktRcv = "FALSE"
ethernet2.addressType = "generated"
ethernet2.pciSlotNumber = "1026"
ethernet3.present = "(ETH3)"
ethernet3.connectionType = "(NETTYPE)"
ethernet3.virtualDev = "vmxnet3"
ethernet3.wakeOnPcktRcv = "FALSE"
ethernet3.addressType = "generated"
ethernet3.pciSlotNumber = "1027"
usb.present = "FALSE"
mks.enable3d = "FALSE"
svga.graphicsMemoryKB = "786432"
pciBridge0.present = "TRUE"
pciBridge4.present = "TRUE"
pciBridge4.virtualDev = "pcieRootPort"
pciBridge4.functions = "8"
pciBridge5.present = "TRUE"
pciBridge5.virtualDev = "pcieRootPort"
pciBridge5.functions = "8"
pciBridge6.present = "TRUE"
pciBridge6.virtualDev = "pcieRootPort"
pciBridge6.functions = "8"
pciBridge7.present = "TRUE"
hpet0.present = "FALSE"
usb.vbluetooth.startConnected = "TRUE"
displayName = "(NAME)"
guestOS = "Other"
nvram = "(NVRAM)"
virtualHW.productCompatibility = "hosted"
powerType.powerOff = "soft"
powerType.powerOn = "soft"
powerType.suspend = "soft"
powerType.reset = "soft"
replay.supported = "FALSE"
replay.filename = ""
sata0:0.redo = ""
pciBridge0.pciSlotNumber = "17"
pciBridge4.pciSlotNumber = "21"
pciBridge5.pciSlotNumber = "22"
pciBridge6.pciSlotNumber = "23"
pciBridge7.pciSlotNumber = "24"
scsi0.pciSlotNumber = "16"
usb.pciSlotNumber = "32"
vmci0.pciSlotNumber = "36"
sata0.pciSlotNumber = "37"
vmci0.id = "-1035185677"
monitor.phys_bits_used = "42"
vmotion.checkpointFBSize = "33554432"
vmotion.checkpointSVGAPrimarySize = "33554432"
cleanShutdown = "TRUE"
softPowerOff = "FALSE"
usb:0.present = "FALSE"
usb:0.deviceType = "hid"
usb:0.port = "0"
usb:0.parent = "-1"
usb:1.speed = "2"
usb:1.present = "FALSE"
usb:1.deviceType = "hub"
usb:1.port = "1"
usb:1.parent = "-1"
numvcpus = "(CPU)"
sata0:1.present = "FALSE"
ehci.present = "FALSE"
sound.present = "FALSE"
serial0.present = "TRUE"
serial0.fileType = "pipe"
serial0.fileName = "(SERIALFILE)"
floppy0.present = "FALSE"
extendedConfigFile = "(EXTCONFIG)"
log.fileName = "(LOGILE)"
msg.autoAnswer = "TRUE"
rtc.diffFromUTC = 0
vhv.enable = "(VHVENABLED)"
vpmc.enable = "(VPMCENABLED)"
`

// GenerateVMX returns a temporary vmx file for creation of vm
func GenerateVMX(cores, memory, disk, name, dir string, numberOfNetworkCards int, networkType string, id string) string {
	replace := func(in, replace, with string) string {
		return strings.Replace(in, replace, with, -1)
	}

	vmx := replace(vmxFile, "(DISK)", disk)

	if runtime.GOOS == "windows" {
		hyperVEnabled := false
		virts, _ := virtualizers.Backends()
		for _, v := range virts {
			if v == "hyperv" {
				hyperVEnabled = true
			}
		}
		if !hyperVEnabled {
			vmx = replace(vmx, "(VHVENABLED)", "TRUE")
			vmx = replace(vmx, "(VPMCENABLED)", "TRUE")
		} else {
			vmx = replace(vmx, "(VHVENABLED)", "FALSE")
			vmx = replace(vmx, "(VPMCENABLED)", "FALSE")
		}
	} else {
		vmx = replace(vmx, "(VHVENABLED)", "TRUE")
		vmx = replace(vmx, "(VPMCENABLED)", "TRUE")
	}
	vmx = replace(vmx, "(CPU)", cores)
	vmx = replace(vmx, "(NAME)", name)
	vmx = replace(vmx, "(MEM)", memory)
	vmx = replace(vmx, "(NVRAM)", dir+"/"+name+".nvram")
	vmx = replace(vmx, "(EXTCONFIG)", dir+"/"+name+".vmxf")
	vmx = replace(vmx, "(LOGILE)", dir+"/"+name+".log")
	if runtime.GOOS == "windows" {
		vmx = replace(vmx, "(SERIALFILE)", "\\\\.\\pipe\\"+id)
	} else {
		vmx = replace(vmx, "(SERIALFILE)", dir+"/socket")
	}

	if numberOfNetworkCards > 0 {
		vmx = replace(vmx, "(ETH0)", "true")
	} else {
		vmx = replace(vmx, "(ETH0)", "false")
	}

	if numberOfNetworkCards > 1 {
		vmx = replace(vmx, "(ETH1)", "true")
	} else {
		vmx = replace(vmx, "(ETH1)", "false")
	}

	if numberOfNetworkCards > 2 {
		vmx = replace(vmx, "(ETH2)", "true")
	} else {
		vmx = replace(vmx, "(ETH2)", "false")
	}

	if numberOfNetworkCards > 3 {
		vmx = replace(vmx, "(ETH3)", "true")
	} else {
		vmx = replace(vmx, "(ETH3)", "false")
	}

	vmx = replace(vmx, "(NETTYPE)", networkType)

	return vmx
}
