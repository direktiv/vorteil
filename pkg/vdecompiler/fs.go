package vdecompiler

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type fsInfo struct {
	superblock *Superblock
	bgdt       []*BGD
}

// ..
const (
	SuperblockOffset = 1024
	BGDSize          = 32

	VorteilUserID        = 1000
	VorteilGroupID       = 1000
	VorteilUserName      = "vorteil"
	VorteilGroupName     = "vorteil"
	InodePermissionsMask = 0777

	InodeTypeDirectory   = 0x4000
	InodeTypeRegularFile = 0x8000
	InodeTypeSymlink     = 0xA000
	InodeTypeMask        = 0xF000

	InodeSize = 128

	RootDirInode = 2
)

// Superblock ..
type Superblock struct {
	TotalInodes         uint32
	TotalBlocks         uint32
	ReservedBlocks      uint32
	UnallocatedBlocks   uint32
	UnallocatedInodes   uint32
	SuperblockNumber    uint32
	BlockSize           uint32
	FragmentSize        uint32
	BlocksPerGroup      uint32
	FragmentsPerGroup   uint32
	InodesPerGroup      uint32
	LastMountTime       uint32
	LastWrittenTime     uint32
	MountsSinceCheck    uint16
	MountsCheckInterval uint16
	Signature           uint16
	State               uint16
	ErrorProtocol       uint16
	VersionMinor        uint16
	TimeLastCheck       uint32
	TimeCheckInterval   uint32
	OS                  uint32
	VersionMajor        uint32
	SuperUser           uint16
	SuperGroup          uint16
	Padding             [1024 - 84]byte
}

// BGD ..
type BGD struct {
	BlockBitmapBlockAddr uint32
	InodeBitmapBlockAddr uint32
	InodeTableBlockAddr  uint32
	UnallocatedBlocks    uint16
	UnallocatedInodes    uint16
	Directories          uint16
	_                    [14]byte
}

// Inode ..
type Inode struct {
	Mode             uint16
	UID              uint16
	Size             uint32
	LastAccessTime   uint32
	CreationTime     uint32
	ModificationTime uint32
	DeletionTime     uint32
	GID              uint16
	HardLinks        uint16
	Sectors          uint32
	Flags            uint32
	OS1              uint32
	BlockPointer     [12]uint32
	FirstIndirect    uint32
	SecondIndirect   uint32
	ThirdIndirect    uint32
	Generation       uint32
	FileACL          uint32
	SizeUpper        uint32
	BlockAddress     uint32
	OS2              [12]byte
}

// IsRegularFile ..
func (inode *Inode) IsRegularFile() bool {
	return inode.Mode&InodeTypeRegularFile == inode.Mode&InodeTypeMask
}

// IsDirectory ..
func (inode *Inode) IsDirectory() bool {
	return inode.Mode&InodeTypeDirectory == inode.Mode&InodeTypeMask
}

// IsSymlink ..
func (inode *Inode) IsSymlink() bool {
	return inode.Mode&InodeTypeSymlink == InodeTypeSymlink&InodeTypeMask
}

// Dirent ..
type Dirent struct {
	Inode   uint32
	Size    uint16
	NameLen uint8
	Type    uint8
}

// DirectoryEntry ..
type DirectoryEntry struct {
	Inode int
	Type  uint8
	Name  string
}

// Fullsize ..
func (inode *Inode) Fullsize() int {
	return int((int64(inode.SizeUpper) << 32) + int64(inode.Size))
}

// Permissions ..
func (inode *Inode) Permissions() string {
	mode := []byte("----------")

	if inode.IsDirectory() {
		mode[0] = 'd'
	} else if inode.IsSymlink() {
		mode[0] = 'l'
	}

	modeChars := []byte{'r', 'w', 'x'}
	for i := 0; i < 9; i++ {
		if (inode.Mode & (1 << (8 - i))) > 0 {
			mode[1+i] = modeChars[i%3]
		}
	}

	return string(mode)
}

// Superblock ..
func (iio *IO) Superblock(index int) (*Superblock, error) {

	if index == 0 && iio.fs.superblock != nil {
		return iio.fs.superblock, nil
	}

	if iio.fs.superblock == nil {
		entry, err := iio.GPTEntry(FilesystemPartitionName)
		if err != nil {
			return nil, err
		}

		_, err = iio.img.Seek(int64(entry.FirstLBA*SectorSize+SuperblockOffset), io.SeekStart)
		if err != nil {
			return nil, err
		}

		sb := new(Superblock)
		err = binary.Read(iio.img, binary.LittleEndian, sb)
		if err != nil {
			return nil, err
		}

		if sb.Signature != 0xEF53 {
			return nil, errors.New("superblock doesn't contain a valid ext file-system signature (magic number)")
		}

		iio.fs.superblock = sb

	}

	if index == 0 {
		return iio.fs.superblock, nil
	}

	bpg := int(iio.fs.superblock.BlocksPerGroup)
	bs := int(1024 << iio.fs.superblock.BlockSize)

	// TODO: check that index isn't out of bounds

	entry, err := iio.GPTEntry(FilesystemPartitionName)
	if err != nil {
		return nil, err
	}

	_, err = iio.img.Seek(int64(int(entry.FirstLBA)*SectorSize+SuperblockOffset+(bs*bpg*index)), io.SeekStart)
	if err != nil {
		return nil, err
	}

	sb := new(Superblock)
	err = binary.Read(iio.img, binary.LittleEndian, sb)
	if err != nil {
		return nil, err
	}

	if sb.Signature != 0xEF53 {
		return nil, errors.New("superblock doesn't contain a valid ext file-system signature (magic number)")
	}

	return sb, nil
}

// BGDT ..
func (iio *IO) BGDT(index int) ([]*BGD, error) {

	if index == 0 && iio.fs.bgdt != nil {
		return iio.fs.bgdt, nil
	}

	if iio.fs.bgdt == nil {
		sb, err := iio.Superblock(0)
		if err != nil {
			return nil, err
		}

		block := 1
		if sb.BlockSize == 0 {
			block++
		}

		lba, err := iio.BlockToLBA(block)
		if err != nil {
			return nil, err
		}

		_, err = iio.img.Seek(int64(lba*SectorSize), io.SeekStart)
		if err != nil {
			return nil, err
		}

		descriptor := new(BGD)
		err = binary.Read(iio.img, binary.LittleEndian, descriptor)
		if err != nil {
			return nil, err
		}

		bgdtSize := int((int(descriptor.BlockBitmapBlockAddr)-block)*int(1024<<sb.BlockSize)) / BGDSize
		bgdt := make([]*BGD, bgdtSize)
		bgdt[0] = descriptor
		for i := 1; i < bgdtSize; i++ {
			descriptor = new(BGD)
			err = binary.Read(iio.img, binary.LittleEndian, descriptor)
			if err != nil {
				return nil, err
			}

			bgdt[i] = descriptor
		}

		iio.fs.bgdt = bgdt
	}

	if index == 0 {
		return iio.fs.bgdt, nil
	}

	bgdtSize := len(iio.fs.bgdt)
	bgdt := make([]*BGD, bgdtSize)

	sb, err := iio.Superblock(0)
	if err != nil {
		return nil, err
	}

	block := 1
	if sb.BlockSize == 0 {
		block++
	}
	block += int(sb.BlocksPerGroup) * index

	lba, err := iio.BlockToLBA(block)
	if err != nil {
		return nil, err
	}

	_, err = iio.img.Seek(int64(lba*SectorSize), io.SeekStart)
	if err != nil {
		return nil, err
	}

	err = binary.Read(iio.img, binary.LittleEndian, bgdt)
	if err != nil {
		return nil, err
	}

	return bgdt, nil
}

// ResolveInode ..
func (iio *IO) ResolveInode(ino int) (*Inode, error) {

	sb, err := iio.Superblock(0)
	if err != nil {
		return nil, err
	}

	bgno := (ino - 1) / int(sb.InodesPerGroup)
	inodeOffset := (ino - 1) % int(sb.InodesPerGroup)

	bgdt, err := iio.BGDT(0)
	if err != nil {
		return nil, err
	}

	firstInodeTableBlock := int(bgdt[bgno].InodeTableBlockAddr)

	// TODO: check for out of bounds

	lba, err := iio.BlockToLBA(firstInodeTableBlock)
	if err != nil {
		return nil, err
	}

	_, err = iio.img.Seek(int64(lba*SectorSize+inodeOffset*InodeSize), io.SeekStart)
	if err != nil {
		return nil, err
	}

	inode := new(Inode)
	err = binary.Read(iio.img, binary.LittleEndian, inode)
	if err != nil {
		return nil, err
	}

	return inode, nil
}

// BlockToLBA ..
func (iio *IO) BlockToLBA(block int) (int, error) {

	entry, err := iio.GPTEntry(FilesystemPartitionName)
	if err != nil {
		return 0, err
	}

	sb, err := iio.Superblock(0)
	if err != nil {
		return 0, err
	}

	sectorsPerBlock := 2 << sb.BlockSize

	return int(entry.FirstLBA) + block*sectorsPerBlock, nil
}

// Readdir ..
func (iio *IO) Readdir(inode *Inode) ([]*DirectoryEntry, error) {
	rdr, err := iio.InodeReader(inode)
	if err != nil {
		return nil, err
	}

	dirent := new(Dirent)
	list := make([]*DirectoryEntry, 0)

	for {
		err = binary.Read(rdr, binary.LittleEndian, dirent)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		l := int(dirent.Size)
		buf := new(bytes.Buffer)
		_, err = io.CopyN(buf, rdr, int64(l-8))
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		name := cstring(buf.Bytes()[:dirent.NameLen])

		if name == "" || dirent.Inode == 0 {
			continue
		}

		list = append(list, &DirectoryEntry{
			Name:  name,
			Type:  dirent.Type,
			Inode: int(dirent.Inode),
		})
	}

	return list, nil
}

// ResolvePathToInodeNo ..
func (iio *IO) ResolvePathToInodeNo(path string) (int, error) {

	path = filepath.Join("/", path)
	path = filepath.ToSlash(path)
	dir, base := filepath.Split(path)
	if (dir == "" || dir == "/" || dir == "\"") && base == "" {
		return RootDirInode, nil
	}

	parent, err := iio.ResolvePathToInodeNo(dir)
	if err != nil {
		return 0, err
	}

	inode, err := iio.ResolveInode(parent)
	if err != nil {
		return 0, err
	}

	list, err := iio.Readdir(inode)
	if err != nil {
		return 0, err
	}

	for _, entry := range list {
		if entry.Name == base {
			return entry.Inode, nil
		}
	}

	return 0, fmt.Errorf("file not found: %s", path)
}

type inodeReader struct {
	iio        *IO
	inode      *Inode
	block      []byte
	blockNo    int
	blockAddrs []int
	eof        bool
}

func (r *inodeReader) Read(p []byte) (n int, err error) {

	if r.eof {
		return 0, io.EOF
	}

	for {
		if r.block == nil || len(r.block) == 0 {
			if r.block != nil {
				r.blockNo++
			}

			// load block
			if r.blockNo >= len(r.blockAddrs) {
				r.eof = true
				return n, nil
			}

			addr := r.blockAddrs[r.blockNo]

			sb, err := r.iio.Superblock(0)
			if err != nil {
				return n, err
			}

			blockSize := int(1024 << sb.BlockSize)

			if addr == 0 {
				r.block = bytes.Repeat([]byte{0}, blockSize)
			} else {
				lba, err := r.iio.BlockToLBA(addr)
				if err != nil {
					return n, err
				}

				_, err = r.iio.img.Seek(int64(lba*SectorSize), io.SeekStart)
				if err != nil {
					return n, err
				}

				buf := new(bytes.Buffer)
				_, err = io.CopyN(buf, r.iio.img, int64(blockSize))
				if err != nil {
					return n, err
				}

				r.block = buf.Bytes()
			}

		}

		k := copy(p[n:], r.block)
		n += k
		r.block = r.block[k:]

		if n == len(p) {
			return n, nil
		}
	}
}

// TODO: fix this so that it loads block pointer blocks on demand, so it will work sequentially more often

type ext4ExtentHeader struct {
	Magic      uint16
	Entries    uint16
	Max        uint16
	Depth      uint16
	Generation uint32
}

type ext4ExtentIdx struct {
	Block  uint32
	LeafLo uint32
	LeafHi uint16
	_      uint16
}

type ext4Extent struct {
	Block uint32
	Len   uint16
	Hi    uint16
	Lo    uint32
}

// InodeReader ..
func (iio *IO) InodeReader(inode *Inode) (io.Reader, error) {

	blockAddrs := make([]int, 0)
	var end bool
	if inode.Sectors == 0 {
		if inode.IsSymlink() && inode.Size <= 60 { // wizardry ext does storing short symlinks in the inode itself
			var s string
			var data []byte
			x := make([]uint32, 15)
			for i := range inode.BlockPointer {
				x[i] = inode.BlockPointer[i]
			}
			x[12] = inode.FirstIndirect
			x[13] = inode.SecondIndirect
			x[14] = inode.ThirdIndirect
			buf := new(bytes.Buffer)
			_ = binary.Write(buf, binary.LittleEndian, x)
			data = buf.Bytes()
			data = data[:inode.Size]
			s = string(data)
			return strings.NewReader(s), nil
		}
		return &inodeReader{
			iio:        iio,
			inode:      inode,
			blockAddrs: blockAddrs,
		}, nil
	}
	sb, err := iio.Superblock(0)
	if err != nil {
		return nil, err
	}

	blockSize := int(1024 << sb.BlockSize)
	blockAddrs = make([]int, (inode.Fullsize()+blockSize-1)/blockSize)

	// check if inode uses an extents tree
	if inode.Flags&0x80000 > 0 {

		c := 0

		var recurse func(data []byte) error
		recurse = func(data []byte) error {
			// read header
			hdr := new(ext4ExtentHeader)
			r := bytes.NewReader(data)
			binary.Read(r, binary.LittleEndian, hdr)
			if hdr.Magic != 0xF30A {
				fmt.Printf("ERROR: %+v\n%+v\n", hdr, inode)
				panic("blah")
				return errors.New("extent node doesn't have magic number")
			}

			if hdr.Depth != 0 {

				for i := 0; i < int(hdr.Entries); i++ {
					index := new(ext4ExtentIdx)
					binary.Read(r, binary.LittleEndian, index)
					if index.Block != 0 {
						return errors.New("extent index data unaccounted for")
					}
					baddr := int(index.LeafLo) + (int(index.LeafHi) << 32)

					lba, err := iio.BlockToLBA(baddr)
					if err != nil {
						return err
					}

					_, err = iio.img.Seek(int64(lba*SectorSize), io.SeekStart)
					if err != nil {
						return err
					}

					buf := new(bytes.Buffer)
					_, err = io.CopyN(buf, iio.img, int64(blockSize))
					if err != nil {
						return err
					}

					block := buf.Bytes()
					err = recurse(block)
					return err
				}
				return nil
			}

			for i := 0; i < int(hdr.Entries); i++ {
				extent := new(ext4Extent)
				binary.Read(r, binary.LittleEndian, extent)
				if extent.Block != 0 && i == 0 {
					return errors.New("extent data unaccounted for")
				}

				baddr := int(extent.Lo) + (int(extent.Hi) << 32)
				l := int(extent.Len)
				for j := 0; j < int(l); j++ {

					if c == len(blockAddrs) {
						fmt.Println("OVERFULL", inode.Fullsize(), len(blockAddrs), extent.Block, extent.Len)
					}

					blockAddrs[c] = baddr + j
					c++
				}
			}

			return nil
		}

		buf := new(bytes.Buffer)
		binary.Write(buf, binary.LittleEndian, inode.BlockPointer[:])
		err = recurse(buf.Bytes())
		if err != nil {
			return nil, err
		}

		out := &inodeReader{
			iio:        iio,
			inode:      inode,
			blockAddrs: blockAddrs,
		}

		return io.LimitReader(out, int64(inode.Size)), nil
	}

	// inode uses classic ext2 bitmaps

	for i, addr := range inode.BlockPointer[:] {
		if i >= len(blockAddrs) {
			end = true
			break
		}
		blockAddrs[i] = int(addr)
	}

	var scanPointers func(pointerBlock, depth int) ([]int, error)
	scanPointers = func(pointerBlock, depth int) ([]int, error) {

		lba, err := iio.BlockToLBA(pointerBlock)
		if err != nil {
			return nil, err
		}

		_, err = iio.img.Seek(int64(lba*SectorSize), io.SeekStart)
		if err != nil {
			return nil, err
		}

		buf := new(bytes.Buffer)
		_, err = io.CopyN(buf, iio.img, int64(blockSize))
		if err != nil {
			return nil, err
		}

		block := buf.Bytes()
		rdr := bytes.NewReader(block)
		var addr uint32
		var list []int

		for {
			err = binary.Read(rdr, binary.LittleEndian, &addr)
			if err == io.EOF {
				return list, nil
			}
			if err != nil {
				return nil, err
			}

			if depth == 0 {
				list = append(list, int(addr))
				continue
			} else if addr == 0 {
				continue
			}

			sub, err := scanPointers(int(addr), depth-1)
			if err != nil {
				return nil, err
			}

			list = append(list, sub...)
		}
	}

	i := 12
	if !end {
		list, err := scanPointers(int(inode.FirstIndirect), 0)
		if err != nil {
			return nil, err
		}

		for _, addr := range list {
			if i >= len(blockAddrs) {
				end = true
				break
			}
			blockAddrs[i] = addr
			i++
		}
	}

	if !end {
		list, err := scanPointers(int(inode.SecondIndirect), 1)
		if err != nil {
			return nil, err
		}

		for _, addr := range list {
			if i >= len(blockAddrs) {
				end = true
				break
			}
			blockAddrs[i] = addr
			i++
		}
	}

	if !end {
		list, err := scanPointers(int(inode.ThirdIndirect), 2)
		if err != nil {
			return nil, err
		}

		for _, addr := range list {
			if i >= len(blockAddrs) {
				end = true
				break
			}
			blockAddrs[i] = addr
			i++
		}
	}

	out := &inodeReader{
		iio:        iio,
		inode:      inode,
		blockAddrs: blockAddrs,
	}

	return io.LimitReader(out, int64(inode.Size)), nil
}
