package vdecompiler

import (
	"archive/tar"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/vorteil/vorteil/pkg/vcfg"
)

type vpartInfo struct {
	cfg   *KernelConfig
	files []*KernelFile
	vcfg  *vcfg.VCFG
}

const (
	maxDNSServers = 4
	maxNICs       = 4
	maxRedirects  = 4
	maxNFSMounts  = 4
	maxNTPServers = 5
)

// VorteilPartitionRegion ..
type VorteilPartitionRegion struct {
	LBA     uint32
	Sectors uint32
}

// KernelConfig ..
type KernelConfig struct {
	Bootloader struct { // 0
		Version        [16]byte  // 0
		_              [12]byte  // 16
		PreloadSectors uint32    // 28
		LinuxArgsLen   uint16    // 32
		_              [222]byte // 34
		LinuxArgs      [256]byte // 256
	}

	Layout struct { // 512
		Configuration                            VorteilPartitionRegion // 512
		Kernel                                   VorteilPartitionRegion // 520
		_/*Trampoline*/ VorteilPartitionRegion   // 528
		_/*Variables*/ VorteilPartitionRegion    // 536
		_/*Arguments*/ VorteilPartitionRegion    // 544
		InitdConfig                              VorteilPartitionRegion // 552
		LoggingConfig                            VorteilPartitionRegion // 560
		_                                        [8]byte                // 568
		VCFGTOML                                 VorteilPartitionRegion // 576
		_/*Application*/ VorteilPartitionRegion  // 584
		_/*ScratchSpace*/ VorteilPartitionRegion // 592
		_                                        [32]byte               // 600
		FileSystem                               VorteilPartitionRegion // 632
		_                                        [384]byte              // 640
	}

	Kernel struct { // 1024
		_/*BootupDelay*/ uint32 // 1024
		MaxFDs                  uint32                   // 1028
		OutputFormat            uint16                   // 1032
		OutputBehaviour         uint16                   // 1034
		DebugFlags              uint32                   // 1036
		DebugSyscallBitmap      [128]byte                // 1040
		_                       [624]byte                // 1168
		NTP                     [maxNTPServers][256]byte // 1792
	}

	App struct { // 3072
		_/*ELFMemoryRequirements*/ uint32 // 3072
		_                                 [60]byte  // 3076
		MetadataVersion                   byte      // 3136
		Name                              [64]byte  // 3137
		Author                            [128]byte // 3201
		Version                           [64]byte  // 3329
		Date                              uint64    // 3393
		URL                               [256]byte // 3401
		Summary                           [280]byte // 3657
		Kernel                            [16]byte  // 3937
		CPUs                              uint8     // 3953
		RAM                               uint32    // 3954
		Inodes                            uint32    // 3958
		DiskSize                          uint32    // 3962
		NetworkPorts                      [96]byte  // 3966
		_                                 [34]byte  // 4062
	}

	VFS struct { // 4096
		FSType                    [8]byte // 4096
		_/*DiskCacheSize*/ uint32 // 4104
		_                         [1012]byte             // 4108
		Redirect                  [maxRedirects]struct { // 5120
			Path [63]byte
			_    byte
			Dest [63]byte
			_    byte
			_    [128]byte
		}
		NFS [maxNFSMounts]struct { // 6144
			MountPoint [127]byte
			_          byte
			Address    [127]byte
			_          byte
			Arguments  [255]byte
			_          byte
		}
		// _ [1024]byte // 7168
	}

	Network struct { // 8192
		Hostname [256]byte              // 8192
		DNS      [maxDNSServers][4]byte // 8448
		_        [752]byte              // 8464
		NICs     [4]struct {            // 9216
			IP                     [4]byte
			Mask                   [4]byte
			Gateway                [4]byte
			MTU                    uint16
			TCPSegmentationOffload bool
			TCPDump                bool
			_                      [240]byte
		}
		Routes [16]struct { // 10240
			Interface   uint32
			Destination [4]byte
			Gateway     [4]byte
			Mask        [4]byte
		}
		_ [1792]byte // 10496
	}
	_ [4096]byte // 12288

}

// KernelConfig ..
func (iio *IO) KernelConfig() (*KernelConfig, error) {

	if iio.vpart.cfg != nil {
		return iio.vpart.cfg, nil
	}

	entry, err := iio.GPTEntry(VorteilPartitionName)
	if err != nil {
		return nil, err
	}

	_, err = iio.img.Seek(int64(entry.FirstLBA*SectorSize), io.SeekStart)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	_, err = io.CopyN(buf, iio.img, 16*1024)
	if err != nil {
		return nil, err
	}

	kcfg := new(KernelConfig)

	err = binary.Read(bytes.NewReader(buf.Bytes()), binary.LittleEndian, kcfg)
	if err != nil {
		return nil, err
	}

	iio.vpart.cfg = kcfg

	return iio.vpart.cfg, nil
}

// KernelFile ..
type KernelFile struct {
	Name        string
	Size        int
	ImageOffset int
}

// KernelFiles ..
func (iio *IO) KernelFiles() ([]*KernelFile, error) {

	if iio.vpart.files != nil {
		return iio.vpart.files, nil
	}

	kcfg, err := iio.KernelConfig()
	if err != nil {
		return nil, err
	}

	k, err := iio.img.Seek(int64(kcfg.Layout.Kernel.LBA*SectorSize), io.SeekStart)
	if err != nil {
		return nil, err
	}

	r := io.LimitReader(iio.img, int64(kcfg.Layout.Kernel.Sectors*SectorSize))

	var kfiles = make([]*KernelFile, 0)

	tr := tar.NewReader(r)

	offset := int(k)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		kf := new(KernelFile)
		kf.Name = hdr.Name
		kf.Size = int(hdr.Size)

		offset += 512
		kf.ImageOffset = offset
		offset += ((int(hdr.Size) + 511) / 512) * 512

		kfiles = append(kfiles, kf)
	}

	iio.vpart.files = kfiles

	return iio.vpart.files, nil
}

// KernelFile ..
func (iio *IO) KernelFile(name string) (io.Reader, error) {

	kfiles, err := iio.KernelFiles()
	if err != nil {
		return nil, err
	}

	idx := -1

	for i, kf := range kfiles {
		if kf.Name == name {
			idx = i
			break
		}
	}

	if idx < 0 {
		return nil, fmt.Errorf("kernel file not found: %s", name)
	}

	_, err = iio.img.Seek(int64(kfiles[idx].ImageOffset), io.SeekStart)
	if err != nil {
		return nil, err
	}

	r := io.LimitReader(iio.img, int64(kfiles[idx].Size))
	return r, nil
}

// VCFGReconstruction ..
func (iio *IO) VCFGReconstruction() (*vcfg.VCFG, error) {

	if iio.vpart.vcfg != nil {
		return iio.vpart.vcfg, nil
	}

	kcfg, err := iio.KernelConfig()
	if err != nil {
		return nil, err
	}

	_, err = iio.img.Seek(int64(kcfg.Layout.VCFGTOML.LBA)*SectorSize, io.SeekStart)
	buf := new(bytes.Buffer)
	_, err = io.CopyN(buf, iio.img, int64(kcfg.Layout.VCFGTOML.Sectors)*SectorSize)
	if err != nil {
		return nil, err
	}

	s := cstring(buf.Bytes())

	cfg, err := vcfg.Load([]byte(s))
	if err != nil {
		return nil, err
	}

	iio.vpart.vcfg = cfg

	return iio.vpart.vcfg, nil
}
