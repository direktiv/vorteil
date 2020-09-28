package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/imageUtils"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vpkg"
)

var imagesCmd = &cobra.Command{
	Use:   "images",
	Short: "Commands for creating and interacting with virtual disk images",
	Long: `The ultimate purpose of any Vorteil app is to become a virtual machine. These
commands are responsible for creating the virtual disk images that are an
important step along the way. These commands also include helper and utility
functions that operate on existing Vorteil virtual disk images.`,
	Aliases: []string{"disks"},
}

var buildCmd = &cobra.Command{
	Use:   "build [BUILDABLE]",
	Short: "Create a virtual disk image",
	Long: `Create a virtual disk image for a Vorteil app.

BUILDABLE refers to anything that can be resolved into a usable source for
building a Vorteil disk vdecompiler. This can include Vorteil project directories and
Vorteil package files. Paths to project directories may optionally include a
direction to determine which project target to build by appending a colon
followed by the target name. In addition to objects available on the local
file-system, BUILDABLE can be a URI identifying an app in a repository by
specifying the repository name followed by a colon and then the bucket, app,
and optional tag as forward-slash separated strings. If BUILDABLE is not
provided it will be substituted with ".", i.e. the current directory, which
must be a valid Vorteil project.

Supported disk formats include:

	xva, raw, vmdk, stream-optimized-vmdk, vhd, vhd-dynamic
`,
	Aliases: []string{"new", "create", "make"},
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		buildablePath := "."
		if len(args) >= 1 {
			buildablePath = args[0]
		}

		format, err := parseImageFormat(flagFormat)
		genericErrCheck(err, 1)
		suffix := format.Suffix()

		_, base := filepath.Split(strings.TrimSuffix(filepath.ToSlash(buildablePath), "/"))
		outputPath := filepath.Join(".", strings.TrimSuffix(base, vpkg.Suffix)+suffix)
		if flagOutput != "" {
			outputPath = flagOutput
			if !strings.HasSuffix(outputPath, suffix) {
				log.Warnf("file name does not end with '%s' file extension", suffix)
			}
		}

		err = checkValidNewFileOutput(outputPath, flagForce, "output", "-f")
		genericErrCheck(err, 1)

		pkgBuilder, err := getPackageBuilder("BUILDABLE", buildablePath)
		genericErrCheck(err, 1)
		defer pkgBuilder.Close()

		err = modifyPackageBuilder(pkgBuilder)
		genericErrCheck(err, 1)

		pkgReader, err := vpkg.ReaderFromBuilder(pkgBuilder)
		genericErrCheck(err, 1)
		defer pkgReader.Close()

		err = initKernels()
		genericErrCheck(err, 1)

		f, err := os.Create(outputPath)
		genericErrCheck(err, 1)
		defer f.Close()

		err = vdisk.Build(context.Background(), f, &vdisk.BuildArgs{
			PackageReader: pkgReader,
			Format:        format,
			KernelOptions: vdisk.KernelOptions{
				Shell: flagShell,
			},
			Logger: log,
		})
		genericErrCheck(err, 1)

		err = f.Close()
		genericErrCheck(err, 1)

		err = pkgReader.Close()
		genericErrCheck(err, 1)

		// TODO: progress tracking
		log.Printf("created image: %s", outputPath)

	},
}

func init() {
	f := buildCmd.Flags()
	f.BoolVarP(&flagForce, "force", "f", false, "force overwrite of existing files")
	f.StringVarP(&flagOutput, "output", "o", "", "path to put image file")
	f.StringVar(&flagFormat, "format", "vmdk", "disk image format")
	f.BoolVar(&flagShell, "shell", false, "add a busybox shell environment to the image")
}

var decompileCmd = &cobra.Command{
	Use:   "decompile IMAGE OUTPUT",
	Short: "Creatsse a usable project directory from a Vorteil disk vdecompiler.",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {

		srcPath := args[0]
		outPath := args[1]
		if err := runDecompile(srcPath, outPath, flagTouched); err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
	},
}

func init() {
	f := decompileCmd.Flags()
	f.BoolVarP(&flagTouched, "touched", "t", false, "Only extract files that have been 'touched'.")
}

var catCmd = &cobra.Command{
	Use:   "cat IMAGE FILEPATH...",
	Short: "Concatenate files and print on the standard output.",
	Args:  cobra.MinimumNArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		img := args[0]

		// Create Vorteil Image Object From Image
		vImageIO, err := vdecompiler.Open(img)
		genericErrCheck(err, 1)
		defer vImageIO.Close()

		for i := 1; i < len(args); i++ {
			fpath := args[i]

			// Get Reader
			rdr, err := imageUtils.CatImageFile(vImageIO, fpath, flagOS)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}

			// Copy Contents
			_, err = io.Copy(os.Stdout, rdr)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}

		}
	},
}

func init() {
	f := catCmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
}

var cpCmd = &cobra.Command{
	Use:   "cp IMAGE SRC_FILEPATH DEST_FILEPATH",
	Short: "Copy files and directories from an image to your system.",
	Args:  cobra.ExactArgs(3),
	Run: func(cmd *cobra.Command, args []string) {

		img := args[0]

		iio, err := vdecompiler.Open(img)
		genericErrCheck(err, 1)
		defer iio.Close()

		dest := args[2]
		fpath := args[1]

		err = imageUtils.CopyImageFile(iio, fpath, dest, flagOS)
		if err != nil {
			panic(err)
		}
	},
}

func init() {
	f := cpCmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
}

var duCmd = &cobra.Command{
	Use:   "du IMAGE [FILEPATH]",
	Short: "Calculate file space usage.",
	Args:  cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		genericErrCheck(SetNumberModeFlagCMD(cmd), 1)
		img := args[0]

		iio, err := vdecompiler.Open(img)
		genericErrCheck(err, 1)
		defer iio.Close()

		all, err := cmd.Flags().GetBool("all")
		genericErrCheck(err, 1)

		free, err := cmd.Flags().GetBool("free")
		genericErrCheck(err, 1)

		maxDepth, err := cmd.Flags().GetInt("max-depth")
		genericErrCheck(err, 1)

		table := [][]string{{"", ""}}

		var fpath string = "/"
		if len(args) > 1 {
			fpath = args[1]
		}

		duOut, err := imageUtils.DUImageFile(iio, fpath, free, maxDepth, all)
		genericErrCheck(err, 1)

		for i := range duOut.ImageFiles {
			table = append(table, []string{duOut.ImageFiles[i].FilePath, fmt.Sprintf("%s", PrintableSize(duOut.ImageFiles[i].FileSize))})
		}

		PlainTable(table)
		if free {
			log.Printf("Free space: %s", PrintableSize(duOut.FreeSpace))
		}
	},
}

func init() {
	f := duCmd.Flags()
	f.BoolP("all", "a", false, "Write counts for all files, not just directories.")
	f.BoolP("free", "f", false, "Add a free-space estimation at the end.")
	f.IntP("max-depth", "l", -1, "Print the total only if the file is within a certain depth.")
	f.StringP("numbers", "n", "short", "Number printing format")
}

var formatCmd = &cobra.Command{
	Use:   "format IMAGE",
	Short: "Image file format information.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		img := args[0]

		iio, err := vdecompiler.Open(img)
		genericErrCheck(err, 1)
		defer iio.Close()

		format, err := iio.ImageFormat()
		genericErrCheck(err, 1)

		log.Printf("Image file format: %s", format)
	},
}

var fsCmd = &cobra.Command{
	Use:   "fs IMAGE",
	Short: "Summarize the information in the main file-system's metadata.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		genericErrCheck(SetNumberModeFlagCMD(cmd), 1)

		iio, err := vdecompiler.Open(args[0])
		genericErrCheck(err, 1)
		defer iio.Close()

		fsReport, err := imageUtils.FSImageFile(iio)
		genericErrCheck(err, 1)

		log.Printf("First LBA:        \t%s", PrintableSize(fsReport.FirstLBA))
		log.Printf("Last LBA:         \t%s", PrintableSize(fsReport.LastLBA))
		log.Printf("Type:             \t%s", fsReport.Type)
		log.Printf("Block size:       \t%s", PrintableSize(fsReport.BlockSize))
		log.Printf("Blocks allocated: \t%s / %s", PrintableSize(fsReport.BlocksAllocated), PrintableSize(fsReport.BlocksAvaliable))
		log.Printf("Inodes allocated: \t%s / %s", PrintableSize(fsReport.InodesAllocated), PrintableSize(fsReport.InodesAvaliable))

		log.Printf("Block groups:     \t%s", PrintableSize(fsReport.BlockGroups))
		log.Printf("  Max blocks each:\t%s", PrintableSize(fsReport.MaxBlock))
		log.Printf("  Max inodes each:\t%s", PrintableSize(fsReport.MaxInodes))

		// TODO: log.Printf("Expansion ceiling: %s")
		log.Printf("Last mount time:  \t%s", fsReport.LastMountTime)
		log.Printf("Last written time:\t%s", fsReport.LastWriteTime)

		// TODO: files
		// TODO: dirs
		// TODO: free space
	},
}

func init() {
	f := fsCmd.Flags()
	f.StringP("numbers", "n", "short", "Number printing format")
}

var fsimgCmd = &cobra.Command{
	Use:   "fsimg IMAGE DEST",
	Short: "Copy the image's file-system partition.",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		img := args[0]
		dst := args[1]

		iio, err := vdecompiler.Open(img)
		genericErrCheck(err, 1)
		defer iio.Close()

		if err := imageUtils.FSIMGImage(iio, dst); err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
	},
}

var gptCmd = &cobra.Command{
	Use:   "gpt IMAGE",
	Short: "Summarize the information in the GUID Partition Table.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		genericErrCheck(SetNumberModeFlagCMD(cmd), 1)

		img := args[0]

		iio, err := vdecompiler.Open(img)
		genericErrCheck(err, 1)
		defer iio.Close()

		gptOut, err := imageUtils.ImageGPT(iio)
		genericErrCheck(err, 1)

		log.Printf("GPT Header LBA:   \t%s", PrintableSize(gptOut.HeaderLBA))
		log.Printf("Backup LBA:       \t%s", PrintableSize(gptOut.BackupLBA))
		log.Printf("First usable LBA: \t%s", PrintableSize(gptOut.FirstUsableLBA))
		log.Printf("Last usable LBA:  \t%s", PrintableSize(gptOut.LastUsableLBA))
		log.Printf("First entries LBA:\t%s", PrintableSize(gptOut.FirstEntriesLBA))
		log.Printf("Entries:")
		for i, entry := range gptOut.Entries {
			log.Printf("  %d: %s", i, entry.Name)
			log.Printf("     First LBA:\t%s", PrintableSize(int(entry.FirstLBA)))
			log.Printf("     Last LBA: \t%s", PrintableSize(int(entry.LastLBA)))
		}
	},
}

func init() {
	f := gptCmd.Flags()
	f.StringP("numbers", "n", "short", "Number printing format")
}

var lsCmd = &cobra.Command{
	Use:   "ls IMAGE [FILEPATH]",
	Short: "List directory contents.",
	Args:  cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		genericErrCheck(SetNumberModeFlagCMD(cmd), 1)

		var reiterating bool

		all, err := cmd.Flags().GetBool("all")
		if err != nil {
			panic(err)
		}

		almostAll, err := cmd.Flags().GetBool("almost-all")
		if err != nil {
			panic(err)
		}

		long, err := cmd.Flags().GetBool("long")
		if err != nil {
			panic(err)
		}

		recursive, err := cmd.Flags().GetBool("recursive")
		if err != nil {
			panic(err)
		}

		img := args[0]

		iio, err := vdecompiler.Open(img)
		genericErrCheck(err, 1)
		defer iio.Close()

		var fpaths []string
		var inos []int
		var inodes []*vdecompiler.Inode
		var table [][]string
		var entries []*vdecompiler.DirectoryEntry

		var fpath string = "/"
		if len(args) > 1 {
			fpath = args[1]
		}

		if flagOS {
			if fpath != "/" && fpath != "" && fpath != "." {
				log.Errorf("bad FILE_PATH for vorteil partition: %s", fpath)
				os.Exit(1)
			}

			kfiles, err := iio.KernelFiles()
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}

			if long {
				var table [][]string
				table = [][]string{{"", "", "", "", "", "", ""}}
				for _, kf := range kfiles {
					table = append(table, []string{"----------", "?", "-", "-", "-", fmt.Sprintf("%s", PrintableSize(kf.Size)), kf.Name})
				}
				PlainTable(table)
			} else {
				for _, kf := range kfiles {
					log.Printf("%s", kf.Name)
				}
			}
			return
		}

		ino, err := iio.ResolvePathToInodeNo(fpath)
		genericErrCheck(err, 1)

	inoEntry:
		inode, err := iio.ResolveInode(ino)
		genericErrCheck(err, 1)

	inodeEntry:
		if !inode.IsDirectory() {
			if reiterating {
				goto skip
			}

			// TODO: log.Info info about files
			return
		}

		if reiterating {
			log.Infof("")
		}

		entries, err = iio.Readdir(inode)
		genericErrCheck(err, 1)

		if recursive {
			log.Printf("%s:", fpath)
		}

		if long {
			table = [][]string{{"", "", "", "", "", "", ""}}
		}

		for _, entry := range entries {
			if !(all || almostAll) && strings.HasPrefix(entry.Name, ".") {
				continue
			}
			if almostAll && (entry.Name == "." || entry.Name == "..") {
				continue
			}

			if recursive && !(entry.Name == "." || entry.Name == "..") {
				fpaths = append(fpaths, filepath.ToSlash(filepath.Join(fpath, entry.Name)))
			}

			if long {
				child, err := iio.ResolveInode(entry.Inode)
				if err != nil {
					log.Errorf("%v", err)
					os.Exit(1)
				}
				links := "?"

				var uid, gid string
				if child.UID == vdecompiler.VorteilUserID {
					uid = vdecompiler.VorteilUserName
				} else {
					uid = fmt.Sprintf("%d", child.UID)
				}

				if child.GID == vdecompiler.VorteilGroupID {
					gid = vdecompiler.VorteilGroupName
				} else {
					gid = fmt.Sprintf("%d", child.GID)
				}

				ts := fmt.Sprintf("%s", time.Unix(int64(child.ModificationTime), 0))
				size := fmt.Sprintf("%s", PrintableSize(child.Fullsize()))

				table = append(table, []string{child.Permissions(), links, uid, gid, ts, size, entry.Name})

				if recursive && !(entry.Name == "." || entry.Name == "..") {
					inodes = append(inodes, child)
				}
			} else {

				if recursive {
					log.Printf("  %s", entry.Name)
					if !(entry.Name == "." || entry.Name == "..") {
						inos = append(inos, entry.Inode)
					}
				} else {
					log.Printf("%s", entry.Name)
				}
			}

		}

		if long {
			PlainTable(table)
		}

	skip:
		if recursive {
			reiterating = true
			if len(fpaths) > 0 {
				fpath = fpaths[0]
				fpaths = fpaths[1:]
			}
			if len(inos) > 0 {
				ino = inos[0]
				inos = inos[1:]
				goto inoEntry
			}
			if len(inodes) > 0 {
				inode = inodes[0]
				inodes = inodes[1:]
				goto inodeEntry
			}
		}
	},
}

func init() {
	f := lsCmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
	f.BoolP("all", "a", false, "Do not ignore entries starting with \".\".")
	f.BoolP("almost-all", "A", false, "Do not list implied \".\" and \"..\".")
	f.BoolP("long", "l", false, "Use a long listing format.")
	f.BoolP("recursive", "R", false, "List subdirectories recursively.")
	f.StringP("numbers", "n", "short", "Number printing format")
}

var md5Cmd = &cobra.Command{
	Use:   "md5 IMAGE FILEPATH",
	Short: "Compute MD5 checksum for a file on an vdecompiler.",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		img := args[0]

		fpath := args[1]
		imageFileMD5, err := imageUtils.MDSumImageFile(img, fpath, flagOS)
		genericErrCheck(err, 1)

		log.Printf("%s", imageFileMD5)
	},
}

func init() {
	f := md5Cmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
	f.StringP("numbers", "n", "short", "Number printing format")
}

var statCmd = &cobra.Command{
	Use:   "stat IMAGE [FILEPATH]",
	Short: "Print detailed metadata relating to the file at FILE_PATH.",
	Args:  cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		genericErrCheck(SetNumberModeFlagCMD(cmd), 1)

		img := args[0]

		var fpath string = "/"
		if len(args) > 1 {
			fpath = args[1]
		}

		fileStat, err := imageUtils.StatImageFile(img, fpath, flagOS)
		genericErrCheck(err, 1)

		log.Printf("File: %s", fileStat.FileName)
		log.Printf("Size: %s", PrintableSize(fileStat.Size))
		log.Printf("Inode: %d", fileStat.Inode)
		log.Printf("Permissions: %s", fileStat.Permissions)
		log.Printf("Uid: %d (%s)", fileStat.UID, fileStat.User)
		log.Printf("Gid: %d (%s)", fileStat.GID, fileStat.Group)
		log.Printf("Access: %s", fileStat.Access)
		log.Printf("Modify: %s", fileStat.Modify)
		log.Printf("Create: %s", fileStat.Create)
	},
}

func init() {
	f := statCmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
	f.StringP("numbers", "n", "short", "Number printing format")
}

var treeCmd = &cobra.Command{
	Use:   "tree IMAGE [FILEPATH]",
	Short: "List contents of directories in a tree-like format.",
	Args:  cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		img := args[0]

		var fpath string = "/"
		if len(args) > 1 {
			fpath = args[1]
		}

		treeResults, err := imageUtils.TreeImageFile(img, fpath, flagOS)
		genericErrCheck(err, 1)

		log.Printf(treeResults.String())

	},
}

func init() {
	f := treeCmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
}
