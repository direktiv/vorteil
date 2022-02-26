package ext4

import (
	"context"
	"errors"
	"fmt"

	"github.com/vorteil/vorteil/pkg/vio"
)

type planner struct {
	inodeBlocks                              []node
	minFreeInodes, minInodes, minInodesPer64 int64
	minFreeSpace                             int64
	filledDataBlocks                         int64
	minSize                                  int64
}

func calculateMinimumSize(ctx context.Context, minDataBlocks, minInodes, minInodesPer64 int64) (int64, error) {

	var err error
	var journalBlocks, contentBlocks, groups, groupsPerFlex int64
	var maxOverflowBlocks, inodesPerGroup, groupDescriptors int64
	var blocksPerInodeTable, blocksPerBGDT int64
	var overheadBlocksPerFlex, groupZeroOverhead int64
	var blocksPerFlex, maxContentInFlexZero, maxContentInFlexNonZero int64
	var flexNeededToContainContent int64
	var totalBlocks, totalGroups int64

	minDataBlocks++ // one extra block for the resize inode
	journalBlocks = MinJournalBlocks
	contentBlocks = minDataBlocks + journalBlocks
	groups = divide(contentBlocks, BlocksPerGroup)
	groupsPerFlex = 1

	for {

		if err = ctx.Err(); err != nil {
			return 0, err
		}

		inodesPerGroup = divide(minInodes, groups)

		// each block group is 128 MiB, so we double the per64 value if it's set
		if inodesPerGroup < minInodesPer64*2 {
			inodesPerGroup = minInodesPer64 * 2
		}

		inodesPerGroup = align(inodesPerGroup, InodesPerBlock)

		if inodesPerGroup > BlockSize*8 {
			groups++
			continue
		}

		blocksPerInodeTable = divide(inodesPerGroup, InodesPerBlock)

		groupDescriptors = groups
		groupDescriptors *= 1024 // NOTE: default behaviour is to allow the file-system to grow up to 1024 times larger.
		groupDescriptors = align(groupDescriptors, DescriptorsPerBlock)
		if groupDescriptors > MaxGroupDescriptors {
			groupDescriptors = MaxGroupDescriptors
		}

		blocksPerBGDT = divide(groupDescriptors, DescriptorsPerBlock)

		overheadBlocksPerFlex = (2 + blocksPerInodeTable) * groupsPerFlex
		groupZeroOverhead = overheadBlocksPerFlex + blocksPerBGDT + 1
		if groupZeroOverhead > BlocksPerGroup {
			return 0, errors.New("an unusual situation was encountered while calculating the minimum size of an ext4 file-system: you could try reducing the number of inodes or increasing the size of the image, or submit a bug report so we can improve this logic")
		}

		contentBlocks = minDataBlocks + journalBlocks
		blocksPerFlex = groupsPerFlex * BlocksPerGroup
		maxContentInFlexZero = blocksPerFlex - groupZeroOverhead
		maxContentInFlexNonZero = blocksPerFlex - overheadBlocksPerFlex

		flexNeededToContainContent = 1
		if contentBlocks > maxContentInFlexZero {
			flexNeededToContainContent = 1 + divide(contentBlocks-maxContentInFlexZero, maxContentInFlexNonZero)

			// NOTE: An extent tree can fit fully within an inode if it has four or fewer extents. Since
			//		we are writing data in a way that is only fragmented by flex metadata, a file can only
			// 		be fragmented across more than four extents if it is consuming ALL of the content space
			//		across three flex groups (with some spillover into the preceding and following flex).
			// 		This means the number of deep extent trees needed cannot exceed (N-2)/3:
			//		N being the total number of flexes, the -2 being the first and last flex, and the 3 being
			// 		three fully consumed flexes.
			//
			//		We do not factor in the possibility of extent trees adding more than one block to the
			// 		file-system (each) because such files would need to be enormous (at least 42.5 GiB, and
			// 		probably much larger depending on the size of a flex group and the amount of space taken
			//		up by inode tables). Supporting such large files is a problem for another time.
			totalGroups = divide(totalBlocks, BlocksPerGroup)
			maxOverflowBlocks = (totalGroups - 2) / 3 // NOTE: we learned that extents have a max size of 128 MiB... booo!
			contentBlocks += maxOverflowBlocks
			flexNeededToContainContent = 1 + divide(contentBlocks-maxContentInFlexZero, maxContentInFlexNonZero)
		}

		totalBlocks = groupZeroOverhead + overheadBlocksPerFlex*(flexNeededToContainContent-1) + contentBlocks
		if totalBlocks <= (groups-1)*BlocksPerGroup {
			totalBlocks = (groups-1)*BlocksPerGroup + 1
		}

		totalGroups = divide(totalBlocks, BlocksPerGroup)
		if totalGroups > groups {
			groups = totalGroups
			continue
		}

		if groups > 1 && groupsPerFlex == 1 {
			groupsPerFlex = 2
			continue
		}

		if groups%(groupsPerFlex*2) == 0 && (overheadBlocksPerFlex*2+blocksPerBGDT+1) < BlocksPerGroup {
			groupsPerFlex *= 2
			continue
		}

		if journalBlocks < MaxJournalBlocks && journalBlocks < totalBlocks/10 && journalBlocks < maxContentInFlexZero {
			journalBlocks = totalBlocks / 10
			if journalBlocks > MaxJournalBlocks {
				journalBlocks = MaxJournalBlocks
			}
			if journalBlocks > maxContentInFlexZero {
				journalBlocks = maxContentInFlexZero
			}
			continue
		}

		return totalBlocks * BlockSize, nil

	}

}

func (p *planner) commit(ctx context.Context, tree vio.FileTree) error {

	filledDataBlocks, nodeBlocks, err := scanInodes(ctx, tree)
	if err != nil {
		return err
	}
	p.filledDataBlocks = filledDataBlocks
	p.inodeBlocks = nodeBlocks

	minInodes := int64(len(nodeBlocks)) - 1
	minInodes += p.minFreeInodes
	if minInodes < p.minInodes {
		minInodes = p.minInodes
	}

	minDataBlocks := filledDataBlocks
	minDataBlocks += divide(p.minFreeSpace, BlockSize)

	p.minSize, err = calculateMinimumSize(ctx, minDataBlocks, minInodes, p.minInodesPer64)
	if err != nil {
		return err
	}

	return nil

}

func scanInodes(ctx context.Context, tree vio.FileTree) (int64, []node, error) {

	var err error
	var ino, minInodes, filledDataBlocks, delta int64
	ino = 9
	minInodes = 9 + int64(tree.NodeCount())
	inodeBlocks := make([]node, minInodes+1) // +1 because inodes start counting at 1 rather than 0

	err = tree.WalkNode(func(path string, n *vio.TreeNode) error {

		if err = ctx.Err(); err != nil {
			return err
		}

		ino++

		if n.File.IsSymlink() {
			delta = calculateSymlinkBlocks(n.File)
		} else if n.File.IsDir() {
			delta = calculateDirectoryBlocks(n)
		} else {
			delta = calculateRegularFileBlocks(n.File)
		}

		inodeBlocks[ino].start = filledDataBlocks
		inodeBlocks[ino].node = n
		inodeBlocks[ino].content = uint32(delta)
		inodeBlocks[ino].fs = uint32(delta)
		n.NodeSequenceNumber = ino
		filledDataBlocks += delta

		return nil

	})
	if err != nil {
		return 0, nil, err
	}

	// shift the root directory to inode 2
	inodeBlocks[RootDirInode] = inodeBlocks[10]
	inodeBlocks[10] = node{}
	inodeBlocks[RootDirInode].node.NodeSequenceNumber = RootDirInode

	return filledDataBlocks, inodeBlocks, nil

}

func (c *Compiler) setPrecompileConstants(size, minDataBlocks, minInodes, minInodesPer64 int64) error {

	c.size = size
	blocks := c.size / BlockSize
	groups := divide(blocks, BlocksPerGroup)

	if minInodes < 10 {
		minInodes = 10
	}

	groupDescriptors := groups
	groupDescriptors *= 1024 // NOTE: default behaviour is to allow the file-system to grow up to 1024 times larger.
	groupDescriptors = align(groupDescriptors, DescriptorsPerBlock)
	if groupDescriptors > MaxGroupDescriptors {
		groupDescriptors = MaxGroupDescriptors
	}

	groupsPerFlex := int64(1)
	if groups > 1 {
		groupsPerFlex = 2
	}
	for i := int64(2); i <= groups; i = i * 2 {
		if groups%i != 0 {
			break
		}
		groupsPerFlex = i
	}
	flexes := divide(groups, groupsPerFlex)

	inodesPerGroup := divide(minInodes, groups)
	if inodesPerGroup < minInodesPer64*2 {
		inodesPerGroup = minInodesPer64 * 2
	}
	if inodesPerGroup > BlockSize*8 {
		return fmt.Errorf("file-system needs to be much larger to allow space for so many inodes")
	}
	inodesPerGroup = align(inodesPerGroup, InodesPerBlock)

	superOverheadBlocks := 1 + divide(groupDescriptors, DescriptorsPerBlock)
	flexOverheadBlocks := (2 + divide(inodesPerGroup, InodesPerBlock)) * groupsPerFlex
	if superOverheadBlocks+flexOverheadBlocks > BlocksPerGroup {
		return errors.New("an unusual situation was encountered while calculating the layout of an ext4 file-system: you could try reducing the number of inodes or increasing the size of the image, or submit a bug report so we can improve this logic")
	}

	flexZeroContent := groupsPerFlex * BlocksPerGroup
	if blocks < flexZeroContent {
		flexZeroContent = blocks
	}
	flexZeroContent -= superOverheadBlocks
	flexZeroContent -= flexOverheadBlocks

	totalContent := flexZeroContent
	if flexes > 1 {
		totalContent += blocks - groupsPerFlex*BlocksPerGroup
		totalContent -= (flexes - 1) * flexOverheadBlocks
	}

	err := c.initResizeNode()
	if err != nil {
		return err
	}

	err = c.initJournalNode(blocks, flexZeroContent, totalContent-minDataBlocks)
	if err != nil {
		return err
	}

	c.inodeBlocks[RootDirInode].node.Parent = c.inodeBlocks[RootDirInode].node

	c.super.init(blocks, inodesPerGroup, &c.inodeBlocks)

	c.data.init(c.super.inodes)

	return nil

}
