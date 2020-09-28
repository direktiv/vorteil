package imagetools

import (
	"fmt"
	"strings"

	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// TreeReport ...
type TreeReport struct {
	Name     string
	Children []TreeReport
}

func (tR *TreeReport) String() string {
	var lastFile bool
	tStr := fmt.Sprintf("%s\n", tR.Name)
	for i := range tR.Children {
		if i == len(tR.Children)-1 {
			lastFile = true
		}

		tStr = tR.Children[i].string(tStr, 0, lastFile)
	}

	return strings.TrimSpace(tStr)
}

func (tR *TreeReport) string(tStr string, depth int, last bool) string {
	var lastFile bool

	var depthString string
	if depth > 0 {
		depthString = "│" + strings.Repeat("    ", depth)
	}
	depth = depth + 1

	if last {
		tStr = fmt.Sprintf("%s%s└── %s\n", tStr, depthString, tR.Name)
	} else {
		tStr = fmt.Sprintf("%s%s├── %s\n", tStr, depthString, tR.Name)
	}

	for i := range tR.Children {
		if i == len(tR.Children)-1 {
			lastFile = true
		}
		tStr = tR.Children[i].string(tStr, depth, lastFile)

	}

	return tStr
}

// TreeImageFile ...
func TreeImageFile(vorteilImagePath string, imageFilePath string, seekOS bool) (TreeReport, error) {
	vorteilImage, err := vdecompiler.Open(vorteilImagePath)
	if err != nil {
		return TreeReport{}, err
	}
	defer vorteilImage.Close()

	if seekOS {
		return treeImageOSFile(vorteilImage, imageFilePath)
	}

	ino, err := vorteilImage.ResolvePathToInodeNo(imageFilePath)
	if err != nil {
		return TreeReport{}, err
	}

	return treeImageFileRecurse(vorteilImage, ino, imageFilePath)
}

func treeImageOSFile(vorteilImage *vdecompiler.IO, imageFilePath string) (TreeReport, error) {
	var treeOut TreeReport = TreeReport{
		Name:     imageFilePath,
		Children: make([]TreeReport, 0),
	}

	if imageFilePath != "" && imageFilePath != "/" && imageFilePath != "." {
		return treeOut, fmt.Errorf("bad FILE_PATH for vpartition: %s", imageFilePath)
	}

	kfiles, err := vorteilImage.KernelFiles()
	if err != nil {
		return treeOut, err
	}

	for i := 0; i < len(kfiles); i++ {
		treeOut.Children = append(treeOut.Children, TreeReport{
			Name:     kfiles[i].Name,
			Children: make([]TreeReport, 0),
		})
	}

	return treeOut, err
}

func treeImageFileRecurse(vorteilImage *vdecompiler.IO, ino int, name string) (TreeReport, error) {
	var treeOut TreeReport = TreeReport{
		Name:     name,
		Children: make([]TreeReport, 0),
	}

	var skipFirst bool

	var recurse func(int, string, *TreeReport) (TreeReport, error)
	recurse = func(ino int, name string, currentTreeDir *TreeReport) (TreeReport, error) {
		var childDir *TreeReport
		inode, err := vorteilImage.ResolveInode(ino)
		if err != nil {
			return treeOut, err
		}

		if skipFirst {
			currentTreeDir.Children = append(currentTreeDir.Children, TreeReport{
				Name:     name,
				Children: make([]TreeReport, 0),
			})
			childDir = &currentTreeDir.Children[len(currentTreeDir.Children)-1]
		} else {
			childDir = currentTreeDir
			skipFirst = true
		}
		// fmt.Printf("CURRENT CHILD DIR = %s\n", childDir.Name)

		var entries []*vdecompiler.DirectoryEntry

		if inode.IsDirectory() {
			entries, err = vorteilImage.Readdir(inode)
			if err != nil {
				return treeOut, err
			}
		}

		if len(entries) > 2 {
			for i := 2; i < len(entries)-1; i++ {
				treeOut, err = recurse(entries[i].Inode, entries[i].Name, childDir)
				if err != nil {
					break
				}
			}

			if err == nil {
				treeOut, err = recurse(entries[len(entries)-1].Inode, entries[len(entries)-1].Name, childDir)
			}
		}

		return treeOut, err
	}

	return recurse(ino, name, &treeOut)
}
