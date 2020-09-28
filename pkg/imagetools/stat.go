package imagetools

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// TODO StatFileReport - Blocks
// TODO StatFileReport - IO Block
// TODO StatFileReport - Links

// StatFileReport ...
type StatFileReport struct {
	FileName    string
	FileType    string
	Size        int
	Inode       int
	UID         uint16
	User        string
	GID         uint16
	Group       string
	Permissions string
	Access      time.Time
	Modify      time.Time
	Create      time.Time
}

// StatImageFile ...
func StatImageFile(vorteilImagePath string, imageFilePath string, seekOS bool) (StatFileReport, error) {
	var statOut StatFileReport
	vorteilImage, err := vdecompiler.Open(vorteilImagePath)
	if err != nil {
		return statOut, err
	}
	defer vorteilImage.Close()

	if seekOS {
		var s string
		var size int
		ftype := "regular file"

		imageFilePath = strings.TrimPrefix(imageFilePath, "/")
		if imageFilePath == "" {
			s = "/"
			size = 0
		} else {
			kfiles, err := vorteilImage.KernelFiles()
			if err == nil {
				for _, kf := range kfiles {
					if kf.Name == imageFilePath {
						s = imageFilePath
						size = kf.Size
						break
					}
				}
				if s == "" {
					err = fmt.Errorf("kernel file not found: %s", imageFilePath)
				}
			}
			if err != nil {
				return statOut, err
			}
		}

		statOut.FileName = s
		statOut.FileType = ftype
		statOut.Size = size
	} else {
		var inode *vdecompiler.Inode
		ino, err := vorteilImage.ResolvePathToInodeNo(imageFilePath)
		if err == nil {
			inode, err = vorteilImage.ResolveInode(ino)
		}

		if err != nil {
			return statOut, err
		}

		var ftype string

		var user, group string
		user = "?"
		group = "?"
		if inode.UID == vdecompiler.VorteilUserID {
			user = vdecompiler.VorteilUserName
		}
		if inode.GID == vdecompiler.VorteilGroupID {
			group = vdecompiler.VorteilGroupName
		}

		statOut.FileName = filepath.Base(imageFilePath)
		statOut.FileType = ftype
		statOut.Size = inode.Fullsize()
		statOut.Inode = ino
		statOut.Permissions = fmt.Sprintf("%#o/%s", inode.Mode&vdecompiler.InodePermissionsMask, inode.Permissions())
		statOut.UID = inode.UID
		statOut.User = user
		statOut.GID = inode.GID
		statOut.Group = group
		statOut.Access = time.Unix(int64(inode.LastAccessTime), 0)
		statOut.Modify = time.Unix(int64(inode.ModificationTime), 0)
		statOut.Create = time.Unix(int64(inode.CreationTime), 0)
	}
	return statOut, err
}
