package vdecompiler

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/vorteil/vorteil/pkg/ext"
	"github.com/vorteil/vorteil/pkg/vimg"
)

type fsInfo struct {
	superblock *ext.Superblock
	bgdt       []*ext.BlockGroupDescriptorTableEntry
}

func (iio *IO) readSuperblock(index int) (*ext.Superblock, error) {

	entry, err := iio.GPTEntry(UTF16toString(vimg.RootPartitionName))
	if err != nil {
		return nil, err
	}

	var bpg, bs int64
	if index > 0 {
		bpg = int64(iio.fs.superblock.BlocksPerGroup)
		bs = int64(1024 << iio.fs.superblock.BlockSize)
	}

	_, err = iio.img.Seek(int64(int64(entry.FirstLBA)*vimg.SectorSize+ext.SuperblockOffset+(bs*bpg*int64(index))), io.SeekStart)
	if err != nil {
		return nil, err
	}

	sb := new(ext.Superblock)
	err = binary.Read(iio.img, binary.LittleEndian, sb)
	if err != nil {
		return nil, err
	}

	if sb.Signature != ext.Signature {
		return nil, errors.New("superblock doesn't contain a valid ext file-system signature (magic number)")
	}

	return sb, nil

}

// Superblock loads the ext superblock from block group 'index'.
func (iio *IO) Superblock(index int) (*ext.Superblock, error) {

	// only return a cached superblock if index is zero
	if index == 0 && iio.fs.superblock != nil {
		return iio.fs.superblock, nil
	}

	var err error
	if iio.fs.superblock == nil {
		iio.fs.superblock, err = iio.readSuperblock(0)
		if err != nil {
			return nil, err
		}
	}

	if index == 0 {
		return iio.fs.superblock, nil
	}

	// TODO: check that index isn't out of bounds

	return iio.readSuperblock(index)

}

func (iio *IO) readBGDT(index int) ([]*ext.BlockGroupDescriptorTableEntry, error) {

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

	_, err = iio.img.Seek(int64(lba*vimg.SectorSize), io.SeekStart)
	if err != nil {
		return nil, err
	}

	bgs := (sb.TotalBlocks + sb.BlocksPerGroup - 1) / sb.BlocksPerGroup
	bgdt := make([]*ext.BlockGroupDescriptorTableEntry, bgs)
	for i := 0; i < int(bgs); i++ {
		bgdte := new(ext.BlockGroupDescriptorTableEntry)
		err = binary.Read(iio.img, binary.LittleEndian, bgdte)
		if err != nil {
			return nil, err
		}
		bgdt[i] = bgdte
	}

	return bgdt, nil

}

// BGDT loads a block group descriptor table from block group 'index'.
func (iio *IO) BGDT(index int) ([]*ext.BlockGroupDescriptorTableEntry, error) {

	// only return a cached bgdt if index is zero
	if index == 0 && iio.fs.bgdt != nil {
		return iio.fs.bgdt, nil
	}

	var err error
	if iio.fs.bgdt == nil {
		iio.fs.bgdt, err = iio.readBGDT(0)
		if err != nil {
			return nil, err
		}
	}

	if index == 0 {
		return iio.fs.bgdt, nil
	}

	// TODO: check that index isn't out of bounds

	return iio.readBGDT(index)

}

func (iio *IO) superblockAndBGDT() (*ext.Superblock, []*ext.BlockGroupDescriptorTableEntry, error) {

	sb, err := iio.Superblock(0)
	if err != nil {
		return nil, nil, err
	}

	bgdt, err := iio.BGDT(0)
	if err != nil {
		return nil, nil, err
	}

	return sb, bgdt, nil

}

// ResolveInode looks up an inode on the file-system.
func (iio *IO) ResolveInode(ino int) (*ext.Inode, error) {

	sb, bgdt, err := iio.superblockAndBGDT()
	if err != nil {
		return nil, err
	}

	bgno := (ino - 1) / int(sb.InodesPerGroup)
	inodeOffset := (ino - 1) % int(sb.InodesPerGroup)
	firstInodeTableBlock := int(bgdt[bgno].InodeTableBlockAddr)

	// TODO: check for out of bounds

	lba, err := iio.BlockToLBA(firstInodeTableBlock)
	if err != nil {
		return nil, err
	}

	_, err = iio.img.Seek(int64(lba*vimg.SectorSize+inodeOffset*ext.InodeSize), io.SeekStart)
	if err != nil {
		return nil, err
	}

	inode := new(ext.Inode)
	err = binary.Read(iio.img, binary.LittleEndian, inode)
	return inode, err

}

// BlockToLBA converts a file-system block number into an absolute disk LBA.
func (iio *IO) BlockToLBA(block int) (int, error) {

	entry, err := iio.GPTEntry(UTF16toString(vimg.RootPartitionName))
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

// Readdir returns a list of directory entries within a directory.
func (iio *IO) Readdir(inode *ext.Inode) ([]*DirectoryEntry, error) {

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

func (iio *IO) resolveChildInodeNumber(inode *ext.Inode, path string) (int, error) {

	_, base := filepath.Split(path)

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

// ResolvePathToInodeNo translates a filepath into an inode number if it can be
// found on the disk.
func (iio *IO) ResolvePathToInodeNo(path string) (int, error) {

	path = filepath.Join("/", path)
	path = filepath.ToSlash(path)
	dir, base := filepath.Split(path)
	if (dir == "" || dir == "/" || dir == "\"") && base == "" {
		return ext.RootDirInode, nil
	}

	parent, err := iio.ResolvePathToInodeNo(dir)
	if err != nil {
		return 0, err
	}

	inode, err := iio.ResolveInode(parent)
	if err != nil {
		return 0, err
	}

	return iio.resolveChildInodeNumber(inode, path)

}

type Dirent struct {
	Inode   uint32
	Size    uint16
	NameLen uint8
	Type    uint8
}

type DirectoryEntry struct {
	Inode int
	Type  uint8
	Name  string
}

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

func (iio *IO) inInodeSymlink(inode *ext.Inode) (io.Reader, error) {

	var s string
	var data []byte
	x := make([]uint32, 15)
	for i := range inode.DirectPointer {
		x[i] = inode.DirectPointer[i]
	}
	x[12] = inode.SinglyIndirect
	x[13] = inode.DoublyIndirect
	x[14] = inode.TriplyIndirect
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, x)
	data = buf.Bytes()
	data = data[:inode.SizeLower]
	s = string(data)
	return strings.NewReader(s), nil

}

func (iio *IO) emptyInode(inode *ext.Inode) (io.Reader, error) {
	blockAddrs := make([]int, 0)
	return &inodeReader{
		iio:        iio,
		inode:      inode,
		blockAddrs: blockAddrs,
	}, nil
}

func (iio *IO) exploreExtentsTree(hdr *ext4ExtentHeader, blockSize int64, data []byte, blockAddrs *[]int, c *int) error {

	r := bytes.NewReader(data)
	_ = binary.Read(r, binary.LittleEndian, hdr)

	for i := 0; i < int(hdr.Entries); i++ {

		index := new(ext4ExtentIdx)
		binary.Read(r, binary.LittleEndian, index)
		if index.Block != 0 {
			return errors.New("extent index data unaccounted for")
		}
		baddr := int(index.LeafLo) + (int(index.LeafHi) << 32)

		block, err := iio.loadBlock(baddr)
		if err != nil {
			return err
		}

		err = iio.recurseExtentsTree(blockSize, block, blockAddrs, c)
		if err != nil {
			return err
		}

	}

	return nil

}

func (iio *IO) recurseExtentsTree(blockSize int64, data []byte, blockAddrs *[]int, c *int) error {

	// read header
	hdr := new(ext4ExtentHeader)
	r := bytes.NewReader(data)
	_ = binary.Read(r, binary.LittleEndian, hdr)
	if hdr.Magic != 0xF30A {
		return errors.New("extent node doesn't have magic number")
	}

	if hdr.Depth != 0 {
		return iio.exploreExtentsTree(hdr, blockSize, data, blockAddrs, c)
	}

	for i := 0; i < int(hdr.Entries); i++ {
		extent := new(ext4Extent)
		binary.Read(r, binary.LittleEndian, extent)
		if extent.Block != 0 && i == 0 {
			return errors.New("extent data unaccounted for")
		}

		baddr := int(extent.Lo) + (int(extent.Hi) << 32)
		l := int(extent.Len)
		for j := 0; j < int(l) && *c < len(*blockAddrs); (*c), j = (*c + 1), j+1 {
			(*blockAddrs)[*c] = baddr + j // TODO: catch corruption
		}
	}

	return nil

}

func (iio *IO) dataFromExtentsTree(inode *ext.Inode) (io.Reader, error) {

	sb, err := iio.Superblock(0)
	if err != nil {
		return nil, err
	}

	blockSize := int64(1024 << sb.BlockSize)
	blockAddrs := make([]int, (InodeSize(inode)+blockSize-1)/blockSize)

	i := 0

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, inode.DirectPointer[:])
	err = iio.recurseExtentsTree(blockSize, buf.Bytes(), &blockAddrs, &i)
	if err != nil {
		return nil, err
	}

	out := &inodeReader{
		iio:        iio,
		inode:      inode,
		blockAddrs: blockAddrs,
	}

	return io.LimitReader(out, int64(inode.SizeLower)), nil

}

func (iio *IO) seekToBlock(blockNo int) error {

	lba, err := iio.BlockToLBA(blockNo)
	if err != nil {
		return err
	}

	_, err = iio.img.Seek(int64(lba*vimg.SectorSize), io.SeekStart)
	if err != nil {
		return err
	}

	return nil

}

func (iio *IO) loadBlock(blockNo int) ([]byte, error) {

	sb, err := iio.Superblock(0)
	if err != nil {
		return nil, err
	}

	blockSize := int64(1024 << sb.BlockSize)

	err = iio.seekToBlock(blockNo)
	if err != nil {
		return nil, err
	}

	buf := new(bytes.Buffer)
	_, err = io.CopyN(buf, iio.img, int64(blockSize))
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil

}

func (iio *IO) scanPointers(pointerBlock, depth int) ([]int, error) {

	block, err := iio.loadBlock(pointerBlock)
	if err != nil {
		return nil, err
	}
	rdr := bytes.NewReader(block)
	var addr uint32
	var list []int

	for {

		err = binary.Read(rdr, binary.LittleEndian, &addr)
		if err != nil {
			if err == io.EOF {
				err = nil
			} else {
				list = nil
			}
			return list, err
		}

		if depth == 0 {
			list = append(list, int(addr))
			continue
		} else if addr == 0 {
			continue
		}

		sub, err := iio.scanPointers(int(addr), depth-1)
		if err != nil {
			return nil, err
		}

		list = append(list, sub...)

	}

}

func loadBlockPointers(iio *IO, addr, depth int, blockAddrs *[]int, i *int) error {

	if *i < len(*blockAddrs) {
		list, err := iio.scanPointers(addr, depth)
		if err != nil {
			return err
		}

		for j := 0; *i < len(*blockAddrs) && j < len(list); *i, j = *i+1, j+1 {
			(*blockAddrs)[*i] = list[j]
		}
	}

	return nil

}

func (iio *IO) dataFromBlockPointers(inode *ext.Inode) (io.Reader, error) {

	sb, err := iio.Superblock(0)
	if err != nil {
		return nil, err
	}

	blockSize := int64(1024 << sb.BlockSize)
	blockAddrs := make([]int, (InodeSize(inode)+blockSize-1)/blockSize)

	// load direct pointers
	for i := 0; i < len(inode.DirectPointer[:]) && i < len(blockAddrs); i++ {
		blockAddrs[i] = int(inode.DirectPointer[i])
	}

	i := 12

	for depth, addr := range []uint32{inode.SinglyIndirect, inode.DoublyIndirect, inode.TriplyIndirect} {
		err = loadBlockPointers(iio, int(addr), depth, &blockAddrs, &i)
		if err != nil {
			return nil, err
		}
	}

	out := &inodeReader{
		iio:        iio,
		inode:      inode,
		blockAddrs: blockAddrs,
	}

	return io.LimitReader(out, InodeSize(inode)), nil

}

// InodeReader reads all of the data stored for an inode.
func (iio *IO) InodeReader(inode *ext.Inode) (io.Reader, error) {

	if InodeIsSymlink(inode) && inode.Sectors == 0 {
		return iio.inInodeSymlink(inode)
	}

	if inode.Sectors == 0 {
		return iio.emptyInode(inode)
	}

	if inode.Flags&0x80000 > 0 {
		return iio.dataFromExtentsTree(inode)
	}

	return iio.dataFromBlockPointers(inode)

}
