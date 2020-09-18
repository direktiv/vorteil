package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vconvert"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vio"
	"github.com/vorteil/vorteil/pkg/virtualizers/firecracker"
	"github.com/vorteil/vorteil/pkg/vpkg"
	"github.com/vorteil/vorteil/pkg/vproj"
)

var log elog.View

var (
	flagJSON             bool
	flagVerbose          bool
	flagDebug            bool
	flagCompressionLevel uint
	flagForce            bool
	flagExcludeDefault   bool
	flagFormat           string
	flagOutput           string
	flagPlatform         string
	flagGUI              bool
	flagOS               bool
	flagShell            bool
	flagTouched          bool
)

const (
	platformQEMU        = "qemu"
	platformVirtualBox  = "virtualbox"
	platformHyperV      = "hyper-v"
	platformFirecracker = "firecracker"
)

func commandInit() {

	// Here we attack VCFG modification flags to relevant commands. Because of
	// the order Go runs init functions this is the safest place to do this.
	addModifyFlags(buildCmd.Flags())
	addModifyFlags(runCmd.Flags())

	// setup logging across all commands
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "enable verbose output")
	rootCmd.PersistentFlags().BoolVarP(&flagDebug, "debug", "d", false, "enable debug output")
	rootCmd.PersistentFlags().BoolVarP(&flagJSON, "json", "j", false, "enable json output")

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {

		logger := &elog.CLI{}

		if flagJSON {
			logger.DisableTTY = true
			logrus.SetFormatter(&logrus.JSONFormatter{})
		} else {
			logrus.SetFormatter(logger)
		}

		logrus.SetLevel(logrus.TraceLevel)

		if flagDebug {
			logger.IsDebug = true
			logger.IsVerbose = true
		} else if flagVerbose {
			logger.IsVerbose = true
		}

		log = logger

		return nil
	}

	// Here we define some hidden top-level shortcuts.
	rootCmd.AddCommand(commandShortcut(buildCmd))
	rootCmd.AddCommand(commandShortcut(decompileCmd))
	rootCmd.AddCommand(commandShortcut(provisionCmd))
	rootCmd.AddCommand(commandShortcut(packCmd))
	rootCmd.AddCommand(commandShortcut(unpackCmd))
	rootCmd.AddCommand(commandShortcut(convertContainerCmd))
	rootCmd.AddCommand(commandShortcut(importSharedObjectsCmd))

	// Here is the visible command structure definition.
	rootCmd.AddCommand(imagesCmd)
	rootCmd.AddCommand(packagesCmd)
	rootCmd.AddCommand(projectsCmd)
	rootCmd.AddCommand(provisionersCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(initFirecrackerCmd)

	imagesCmd.AddCommand(buildCmd)
	imagesCmd.AddCommand(decompileCmd)
	imagesCmd.AddCommand(provisionCmd)
	imagesCmd.AddCommand(catCmd)
	imagesCmd.AddCommand(cpCmd)
	imagesCmd.AddCommand(duCmd)
	imagesCmd.AddCommand(formatCmd)
	imagesCmd.AddCommand(fsCmd)
	imagesCmd.AddCommand(fsimgCmd)
	imagesCmd.AddCommand(gptCmd)
	imagesCmd.AddCommand(lsCmd)
	imagesCmd.AddCommand(md5Cmd)
	imagesCmd.AddCommand(statCmd)
	imagesCmd.AddCommand(treeCmd)

	packagesCmd.AddCommand(packCmd)
	packagesCmd.AddCommand(unpackCmd)

	projectsCmd.AddCommand(convertContainerCmd)
	projectsCmd.AddCommand(importSharedObjectsCmd)

	provisionersCmd.AddCommand(provisionersDefaultCmd)
	provisionersCmd.AddCommand(provisionersDeleteCmd)
	provisionersCmd.AddCommand(provisionersDescribeCmd)
	provisionersCmd.AddCommand(provisionersExportCmd)
	provisionersCmd.AddCommand(provisionersImportCmd)
	provisionersCmd.AddCommand(provisionersListCmd)
	provisionersCmd.AddCommand(provisionersNewCmd)

	provisionersNewCmd.AddCommand(provisionersNewAmazonEC2Cmd)
	provisionersNewCmd.AddCommand(provisionersNewAzureCmd)
	provisionersNewCmd.AddCommand(provisionersNewGoogleCmd)
}

func commandShortcut(cmd *cobra.Command) *cobra.Command {
	c := *cmd
	c.Aliases = []string{}
	c.Hidden = true
	return &c
}

var rootCmd = &cobra.Command{
	Use:   "vorteil",
	Short: "Vorteil's command-line interface",
	Long: `Vorteil's command-line interface provides a complete set of tools for developers
to create, test, optimize, and build Vorteil apps.`,
}

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
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
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
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		pkgBuilder, err := getPackageBuilder("BUILDABLE", buildablePath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer pkgBuilder.Close()

		err = modifyPackageBuilder(pkgBuilder)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		pkgReader, err := vpkg.ReaderFromBuilder(pkgBuilder)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer pkgReader.Close()

		err = initKernels()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		f, err := os.Create(outputPath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer f.Close()

		err = vdisk.Build(context.Background(), f, &vdisk.BuildArgs{
			PackageReader: pkgReader,
			Format:        format,
			KernelOptions: vdisk.KernelOptions{
				Shell: flagShell,
			},
			Logger: log,
		})
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		err = f.Close()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		err = pkgReader.Close()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

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
	Short: "Create a usable project directory from a Vorteil disk vdecompiler.",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {

		srcPath := args[0]
		outPath := args[1]

		iio, err := vdecompiler.Open(srcPath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		fi, err := os.Stat(outPath)
		if err != nil && !os.IsNotExist(err) {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		var into bool
		if !os.IsNotExist(err) && fi.IsDir() {
			into = true
		}

		fpath := "/"
		dpath := outPath
		if into {
			dpath = filepath.ToSlash(filepath.Join(outPath, filepath.Base(fpath)))
		}

		var counter int

		symlinkCallbacks := make([]func() error, 0)

		var recurse func(int, string, string) error
		recurse = func(ino int, rpath string, dpath string) error {

			inode, err := iio.ResolveInode(ino)
			if err != nil {
				return err
			}

			if flagTouched && inode.LastAccessTime == 0 && !inode.IsDirectory() && rpath != "/" {
				log.Printf("skipping untouched object: %s", rpath)
				return nil
			}

			counter++

			log.Printf("copying %s", rpath)

			if inode.IsRegularFile() {
				fi, err = os.Stat(dpath)
				if !os.IsNotExist(err) {
					if err == nil {
						return fmt.Errorf("file already exists: %s", dpath)
					}
					return err
				}

				f, err := os.Create(dpath)
				if err != nil {
					return err
				}
				defer f.Close()

				rdr, err := iio.InodeReader(inode)
				if err != nil {
					return err
				}

				_, err = io.CopyN(f, rdr, int64(inode.Fullsize()))
				if err != nil {
					return err
				}
				return nil
			}

			if inode.IsSymlink() {

				symlinkCallbacks = append(symlinkCallbacks, func() error {
					rdr, err := iio.InodeReader(inode)
					if err != nil {
						return err
					}
					data, err := ioutil.ReadAll(rdr)
					if err != nil {
						return err
					}

					err = os.Symlink(string(string(data)), dpath)
					if err != nil {
						return err
					}
					return nil
				})
				return nil
			}

			if !inode.IsDirectory() {
				log.Warnf("skipping abnormal file: %s", rpath)
				return nil
			}

			fi, err = os.Stat(dpath)
			if !os.IsNotExist(err) {
				if err == nil {
					return fmt.Errorf("file already exists: %s", dpath)
				}
				return err
			}

			err = os.MkdirAll(dpath, 0777)
			if err != nil {
				return err
			}

			entries, err := iio.Readdir(inode)
			if err != nil {
				return err
			}

			for _, entry := range entries {
				if entry.Name == "." || entry.Name == ".." {
					continue
				}
				err = recurse(entry.Inode, filepath.ToSlash(filepath.Join(rpath, entry.Name)), filepath.Join(dpath, entry.Name))
				if err != nil {
					return err
				}
			}

			return nil
		}

		ino, err := iio.ResolvePathToInodeNo(fpath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		err = recurse(ino, filepath.ToSlash(filepath.Base(fpath)), dpath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		for _, fn := range symlinkCallbacks {
			err = fn()
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}
		}

		if flagTouched && counter <= 1 {
			log.Warnf("No touched files detected. Are you sure this disk has been run?")
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

		iio, err := vdecompiler.Open(img)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		for i := 1; i < len(args); i++ {
			fpath := args[i]
			var rdr io.Reader

			if flagOS {
				fpath = strings.TrimPrefix(fpath, "/")
				rdr, err = iio.KernelFile(fpath)
				if err != nil {
					log.Errorf("%v", err)
					os.Exit(1)
				}
			} else {
				ino, err := iio.ResolvePathToInodeNo(fpath)
				if err != nil {
					log.Errorf("%v", err)
					os.Exit(1)
				}

				inode, err := iio.ResolveInode(ino)
				if err != nil {
					log.Errorf("%v", err)
					os.Exit(1)
				}

				if !inode.IsRegularFile() {
					log.Errorf("\"%s\" is not a regular file", fpath)
					os.Exit(1)
				}

				rdr, err = iio.InodeReader(inode)
				if err != nil {
					log.Errorf("%v", err)
					os.Exit(1)
				}

				rdr = io.LimitReader(rdr, int64(inode.Fullsize()))
			}

			_, err := io.Copy(os.Stdout, rdr)
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
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		dest := args[2]
		fi, err := os.Stat(dest)
		if err != nil && !os.IsNotExist(err) {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		var into bool
		if !os.IsNotExist(err) && fi.IsDir() {
			into = true
		}

		fpath := args[1]
		dpath := dest
		if into {
			dpath = filepath.Join(dest, filepath.Base(fpath))
		}

		if flagOS {
			if fpath != "" && fpath != "/" && fpath != "." {
				// single file
				fpath = strings.TrimPrefix(fpath, "/")
				r, err := iio.KernelFile(fpath)
				f, err := os.Create(dpath)
				defer f.Close()
				_, err = io.Copy(f, r)
				if err != nil {
					log.Errorf("%v", err)
					os.Exit(1)
				}
			} else {
				// entire folder
				kfiles, err := iio.KernelFiles()
				if err != nil {
					log.Errorf("%v", err)
					os.Exit(1)
				}

				err = os.MkdirAll(dpath, 077)
				if err != nil {
					log.Errorf("%v", err)
					os.Exit(1)
				}

				for _, kf := range kfiles {
					r, err := iio.KernelFile(kf.Name)
					if err != nil {
						log.Errorf("%v", err)
						os.Exit(1)
					}

					f, err := os.Create(filepath.Join(dpath, kf.Name))
					if err != nil {
						log.Errorf("%v", err)
						os.Exit(1)
					}
					defer f.Close()

					_, err = io.Copy(f, r)
					if err != nil {
						log.Errorf("%v", err)
						os.Exit(1)
					}

					err = f.Close()
					if err != nil {
						log.Errorf("%v", err)
						os.Exit(1)
					}
				}
			}
			return
		}

		var recurse func(int, string, string) error
		recurse = func(ino int, rpath string, dpath string) error {

			inode, err := iio.ResolveInode(ino)
			if err != nil {
				return err
			}

			if inode.IsRegularFile() {
				f, err := os.Create(dpath)
				if err != nil {
					return err
				}
				defer f.Close()

				rdr, err := iio.InodeReader(inode)
				if err != nil {
					return err
				}

				_, err = io.CopyN(f, rdr, int64(inode.Fullsize()))
				if err != nil {
					return err
				}
				return nil
			}

			if inode.IsSymlink() {

				rdr, err := iio.InodeReader(inode)
				if err != nil {
					return err
				}
				data, err := ioutil.ReadAll(rdr)
				if err != nil {
					return err
				}
				err = os.Symlink(string(data), dpath)
				if err != nil {
					return err
				}
				return nil
			}

			if !inode.IsDirectory() {
				log.Warnf("skipping abnormal file: %s", rpath)
				return nil
			}

			err = os.MkdirAll(dpath, 0777)
			if err != nil {
				return err
			}

			entries, err := iio.Readdir(inode)
			if err != nil {
				return err
			}

			for _, entry := range entries {
				if entry.Name == "." || entry.Name == ".." {
					continue
				}
				err = recurse(entry.Inode, filepath.ToSlash(filepath.Join(rpath, entry.Name)), filepath.Join(dpath, entry.Name))
				if err != nil {
					return err
				}
			}

			return nil
		}

		ino, err := iio.ResolvePathToInodeNo(fpath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		err = recurse(ino, filepath.Base(fpath), dpath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
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
		numbers, err := cmd.Flags().GetString("numbers")
		if err != nil {
			panic(err)
		}

		err = SetNumbersMode(numbers)
		if err != nil {
			log.Errorf("couldn't parse value of --numbers: %v", err)
			os.Exit(1)
		}

		img := args[0]

		iio, err := vdecompiler.Open(img)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		all, err := cmd.Flags().GetBool("all")
		if err != nil {
			panic(err)
		}

		free, err := cmd.Flags().GetBool("free")
		if err != nil {
			panic(err)
		}

		maxDepth, err := cmd.Flags().GetInt("max-depth")
		if err != nil {
			panic(err)
		}

		var table [][]string
		table = [][]string{{"", ""}}

		var depth = 0

		var recurse func(*vdecompiler.Inode, string) (int, error)
		recurse = func(inode *vdecompiler.Inode, name string) (int, error) {

			depth++
			defer func() {
				depth--
			}()

			var size int
			size = int(inode.Sectors) * vdecompiler.SectorSize

			if !inode.IsDirectory() {
				return size, nil
			}

			entries, err := iio.Readdir(inode)
			if err != nil {
				return 0, err
			}

			var delta int
			for i := 2; i < len(entries); i++ {
				entry := entries[i]
				child := filepath.ToSlash(filepath.Join(name, entry.Name))

				cinode, err := iio.ResolveInode(entry.Inode)
				if err != nil {
					return 0, err
				}

				delta, err = recurse(cinode, child)
				if err != nil {
					return 0, err
				}
				if all || inode.IsDirectory() {
					if (maxDepth >= 0 && depth <= maxDepth) || maxDepth < 0 {
						table = append(table, []string{child, fmt.Sprintf("%s", PrintableSize(delta))})
					}
				}
				size += delta
			}

			return size, nil
		}

		var fpath string
		if len(args) > 1 {
			fpath = args[1]
		} else {
			fpath = "/"
		}

		ino, err := iio.ResolvePathToInodeNo(fpath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		inode, err := iio.ResolveInode(ino)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		size, err := recurse(inode, fpath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		table = append(table, []string{fpath, fmt.Sprintf("%s", PrintableSize(size))})

		PlainTable(table)

		if free {
			sb, err := iio.Superblock(0)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}

			leftover := int(sb.UnallocatedBlocks) * int(1024<<sb.BlockSize)
			log.Printf("Free space: %s", PrintableSize(leftover))
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
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		format, err := iio.ImageFormat()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		log.Printf("Image file format: %s", format)
	},
}

var fsCmd = &cobra.Command{
	Use:   "fs IMAGE",
	Short: "Summarize the information in the main file-system's metadata.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		numbers, err := cmd.Flags().GetString("numbers")
		if err != nil {
			panic(err)
		}

		err = SetNumbersMode(numbers)
		if err != nil {
			log.Errorf("couldn't parse value of --numbers: %v", err)
			os.Exit(1)
		}

		img := args[0]

		iio, err := vdecompiler.Open(img)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		entry, err := iio.GPTEntry(vdecompiler.FilesystemPartitionName)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		sb, err := iio.Superblock(0)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		log.Printf("First LBA:        \t%s", PrintableSize(int(entry.FirstLBA)))
		log.Printf("Last LBA:         \t%s", PrintableSize(int(entry.LastLBA)))
		log.Printf("Type:             \text2")

		blocksize := 1024 << int(sb.BlockSize)
		log.Printf("Block size:       \t%s", PrintableSize(blocksize))
		log.Printf("Blocks allocated: \t%s / %s", PrintableSize(int(sb.TotalBlocks-sb.UnallocatedBlocks)), PrintableSize(int(sb.TotalBlocks)))
		log.Printf("Inodes allocated: \t%s / %s", PrintableSize(int(sb.TotalInodes-sb.UnallocatedInodes)), PrintableSize(int(sb.TotalInodes)))

		log.Printf("Block groups:     \t%s", PrintableSize(int((sb.TotalBlocks+sb.BlocksPerGroup-1)/sb.BlocksPerGroup)))
		log.Printf("  Max blocks each:\t%s", PrintableSize(int(sb.BlocksPerGroup)))
		log.Printf("  Max inodes each:\t%s", PrintableSize(int(sb.InodesPerGroup)))

		// TODO: log.Printf("Expansion ceiling: %s")
		log.Printf("Last mount time:  \t%s", time.Unix(int64(sb.LastMountTime), 0))
		log.Printf("Last written time:\t%s", time.Unix(int64(sb.LastWrittenTime), 0))

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

		f, err := os.Create(dst)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer f.Close()

		iio, err := vdecompiler.Open(img)
		if err != nil {
			_ = os.Remove(f.Name())
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		rdr, err := iio.PartitionReader(vdecompiler.FilesystemPartitionName)
		if err != nil {
			_ = os.Remove(f.Name())
			log.Errorf("%v", err)
			os.Exit(1)
		}

		_, err = io.Copy(f, rdr)
		if err != nil {
			_ = os.Remove(f.Name())
			log.Errorf("%v", err)
			os.Exit(1)
		}
	},
}

var gptCmd = &cobra.Command{
	Use:   "cat IMAGE",
	Short: "Summarize the information in the GUID Partition Table.",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		numbers, err := cmd.Flags().GetString("numbers")
		if err != nil {
			panic(err)
		}

		err = SetNumbersMode(numbers)
		if err != nil {
			log.Errorf("couldn't parse value of --numbers: %v", err)
			os.Exit(1)
		}

		img := args[0]

		iio, err := vdecompiler.Open(img)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		header, err := iio.GPTHeader()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		entries, err := iio.GPTEntries()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		log.Printf("GPT Header LBA:   \t%s", PrintableSize(int(header.CurrentLBA)))
		log.Printf("Backup LBA:       \t%s", PrintableSize(int(header.BackupLBA)))
		log.Printf("First usable LBA: \t%s", PrintableSize(int(header.FirstUsableLBA)))
		log.Printf("Last usable LBA:  \t%s", PrintableSize(int(header.LastUsableLBA)))
		log.Printf("First entries LBA:\t%s", PrintableSize(int(header.FirstEntriesLBA)))
		log.Printf("Entries:")
		for i, entry := range entries {
			name := entry.NameString()
			if name != "" {
				log.Printf("  %d: %s", i, name)
				log.Printf("     First LBA:\t%s", PrintableSize(int(entry.FirstLBA)))
				log.Printf("     Last LBA: \t%s", PrintableSize(int(entry.LastLBA)))
			}
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
		numbers, err := cmd.Flags().GetString("numbers")
		if err != nil {
			panic(err)
		}

		err = SetNumbersMode(numbers)
		if err != nil {
			log.Errorf("couldn't parse value of --numbers: %v", err)
			return
		}

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
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		var fpaths []string
		var inos []int
		var inodes []*vdecompiler.Inode
		var table [][]string
		var entries []*vdecompiler.DirectoryEntry

		var fpath string
		if len(args) > 1 {
			fpath = args[1]
		} else {
			fpath = "/"
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
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

	inoEntry:
		inode, err := iio.ResolveInode(ino)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

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
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

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

		iio, err := vdecompiler.Open(img)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		fpath := args[1]
		var rdr io.Reader

		if flagOS {
			fpath = strings.TrimPrefix(fpath, "/")
			rdr, err = iio.KernelFile(fpath)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}
		} else {
			ino, err := iio.ResolvePathToInodeNo(fpath)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}

			inode, err := iio.ResolveInode(ino)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}

			if inode.IsDirectory() {
				log.Errorf("\"%s\" is not a regular file", fpath)
				os.Exit(1)
			}

			rdr, err = iio.InodeReader(inode)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}

			rdr = io.LimitReader(rdr, int64(inode.Fullsize()))
		}

		hasher := md5.New()
		_, err = io.Copy(hasher, rdr)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		log.Printf("%s", hex.EncodeToString(hasher.Sum(nil)))
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
		numbers, err := cmd.Flags().GetString("numbers")
		if err != nil {
			panic(err)
		}

		err = SetNumbersMode(numbers)
		if err != nil {
			log.Errorf("couldn't parse value of --numbers: %v", err)
			return
		}

		img := args[0]

		iio, err := vdecompiler.Open(img)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		fpath := "/"
		if len(args) > 1 {
			fpath = args[1]
		}

		if flagOS {
			var s string
			var size int
			ftype := "regular file"

			fpath = strings.TrimPrefix(fpath, "/")
			if fpath == "" {
				s = "/"
				size = 0
			} else {
				kfiles, err := iio.KernelFiles()
				if err != nil {
					log.Errorf("%v", err)
					os.Exit(1)
				}

				for _, kf := range kfiles {
					if kf.Name == fpath {
						s = fpath
						size = kf.Size
						break
					}
				}

				if s == "" {
					log.Errorf("kernel file not found: %s", fpath)
					os.Exit(1)
				}
			}

			log.Printf("File: %s\t%s", s, ftype)
			log.Printf("Size: %s", PrintableSize(size))
			log.Printf("Inode: -")
			log.Printf("Access: -")
			log.Printf("Uid: -")
			log.Printf("Gid: -")
			log.Printf("Access: -")
			log.Printf("Modify: -")
			log.Printf("Create: -")

		} else {
			ino, err := iio.ResolvePathToInodeNo(fpath)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}

			inode, err := iio.ResolveInode(ino)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
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

			log.Printf("File: %s\t%s", filepath.Base(fpath), ftype)
			log.Printf("Size: %s", PrintableSize(inode.Fullsize()))
			// TODO: log.Printf("Blocks: %s", PrintableSize(int()))
			// TODO: log.Printf("IO Block: %s", PrintableSize())
			log.Printf("Inode: %d", ino)
			// TODO: log.Printf("Links: %s")
			log.Printf("Access: %#o/%s", inode.Mode&vdecompiler.InodePermissionsMask, inode.Permissions())
			log.Printf("Uid: %d (%s)", inode.UID, user)
			log.Printf("Gid: %d (%s)", inode.GID, group)
			log.Printf("Access: %s", time.Unix(int64(inode.LastAccessTime), 0))
			log.Printf("Modify: %s", time.Unix(int64(inode.ModificationTime), 0))
			log.Printf("Create: %s", time.Unix(int64(inode.CreationTime), 0))
		}
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

		iio, err := vdecompiler.Open(img)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer iio.Close()

		var fpath string
		if len(args) > 1 {
			fpath = args[1]
		} else {
			fpath = "/"
		}

		if flagOS {
			if fpath != "" && fpath != "/" && fpath != "." {
				log.Errorf("bad FILE_PATH for vpartition: %s", fpath)
				return
			}

			kfiles, err := iio.KernelFiles()
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}

			log.Printf(fpath)

			for i := 0; i < len(kfiles)-1; i++ {
				log.Printf("├── %s", kfiles[i].Name)
			}

			log.Printf("└── %s", kfiles[len(kfiles)-1].Name)
			return
		}

		var code []byte

		var recurse func(int, string) error
		recurse = func(ino int, name string) error {
			inode, err := iio.ResolveInode(ino)
			if err != nil {
				return err
			}

			prefix := ""
			idx := len(code) - 1

			for i, c := range code {
				switch c {
				case 0:
					prefix += "    "
				case 1:
					prefix += "│   "
				case 2:
					if i == idx {
						prefix += "├── "
					} else {
						prefix += "│   "
					}
				case 3:
					if i == idx {
						prefix += "└── "
					} else {
						prefix += "    "
					}
				}
			}

			log.Printf("%s%s", prefix, name)

			if !inode.IsDirectory() {
				return nil
			}

			entries, err := iio.Readdir(inode)
			if err != nil {
				return err
			}

			if len(entries) > 2 {
				idx++
				code = append(code, 2)

				for i := 2; i < len(entries)-1; i++ {
					err = recurse(entries[i].Inode, entries[i].Name)
					if err != nil {
						return err
					}
				}

				code[idx] = 3
				err = recurse(entries[len(entries)-1].Inode, entries[len(entries)-1].Name)
				if err != nil {
					return err
				}

				code = code[:idx]
				idx--
			}

			return nil
		}

		ino, err := iio.ResolvePathToInodeNo(fpath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		err = recurse(ino, fpath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
	},
}

func init() {
	f := treeCmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
}

var provisionCmd = &cobra.Command{
	Use: "provision",
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
		fmt.Println("TODO")
	},
}

var packagesCmd = &cobra.Command{
	Use:   "packages",
	Short: "Create and interact with Vorteil packages",
	Long: `Vorteil packages are compressed and optimized archives containing all of the
information needed to construct a Vorteil virtual disk vdecompiler. They generally
represent an immutable application that can be expected to operate identically
on all supported hypervisors, and they can include metadata and information that
helps to identify it and explain its purpose and its use.

The packages subcommand is a parent to all package related commands and
functionality in the CLI including creating new packages, unpacking the
contents of an existing package, and probing package files for a quick summary
of the application within.`,
}

var packCmd = &cobra.Command{
	Use:     "pack [PACKABLE]",
	Aliases: []string{"pack", "package"},
	Short:   "Create a Vorteil package",
	Long: `Create a new Vorteil package from a project. Vorteil packages are
compressed and optimized archives containing all of the information needed to
construct a Vorteil virtual disk vdecompiler. They generally represent an immutable
application that can be expected to operate identically on all supported
hypervisors, and they can include metadata and information that helps to
identify it and explain its purpose and its use.`,
	Args: cobra.RangeArgs(0, 1),
	Run: func(cmd *cobra.Command, args []string) {

		packablePath := "."
		if len(args) >= 1 {
			packablePath = args[0]
		}

		suffix := ".vorteil"
		_, base := filepath.Split(strings.TrimSuffix(filepath.ToSlash(packablePath), "/"))
		outputPath := filepath.Join(".", strings.TrimSuffix(base, suffix)+suffix)
		if flagOutput != "" {
			outputPath = flagOutput
			if !strings.HasSuffix(outputPath, suffix) {
				log.Warnf("file name does not end with '%s' file extension", suffix)
			}
		}

		err := checkValidNewFileOutput(outputPath, flagForce, "output", "-f")
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		// TODO: other packable type & project targets
		proj, err := vproj.LoadProject(packablePath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		tgt, err := proj.Target("")
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		// TODO: project & vcfg flags

		builder, err := tgt.NewBuilder()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer builder.Close()

		builder.SetCompressionLevel(int(flagCompressionLevel))

		f, err := os.Create(outputPath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer f.Close()

		err = builder.Pack(f)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		err = f.Close()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		err = builder.Close()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		// TODO: progress tracking

		log.Printf("created package: %s", outputPath)
	},
}

func handleFileInjections(builder vpkg.Builder) error {
	for src, v := range filesMap {
		for _, dst := range v {

			stat, err := os.Stat(src)
			if err != nil {
				return err
			}

			if stat.IsDir() {
				// create subtree
				tree, err := vio.FileTreeFromDirectory(src)
				if err != nil {
					return err
				}

				err = builder.AddSubTreeToFS(dst, tree)
				if err != nil {
					return err
				}

			} else {
				// create file object
				f, err := vio.LazyOpen(src)
				if err != nil {
					return err
				}
				defer f.Close()

				err = builder.AddToFS(dst, f)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func init() {
	f := packCmd.Flags()
	f.BoolVarP(&flagForce, "force", "f", false, "force overwrite of existing files")
	f.StringVarP(&flagOutput, "output", "o", "", "path to put package file")
	f.UintVar(&flagCompressionLevel, "compression-level", 1, "compression level (0-9)")
}

var unpackCmd = &cobra.Command{
	Use:     "unpack PACKAGE [DEST]",
	Aliases: []string{"extract"},
	Short:   "Unpack a Vorteil package",
	Long: `Unpack the contents of a Vorteil package into a directory. To simplify
subsequent commands the unpacked files will be organized into a new project
automatically.

The PACKAGE argument must be a path to a Vorteil package. If DEST is
provided it must be a path to a directory that is not already a Vorteil project,
or path to a file that does not exist and could be created without deleting any
other files. If the DEST argument is omitted it will default to ".".`,
	Args: cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {

		pkgPath := args[0]
		prjPath := "."
		if len(args) >= 2 {
			prjPath = args[1]
		}

		err := checkValidNewDirOutput(prjPath, flagForce, "DEST", "-f")
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		pkg, err := vpkg.Open(pkgPath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer pkg.Close()

		err = vproj.CreateFromPackage(prjPath, pkg)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		// TODO: progress tracking

		log.Printf("unpacked to: %s", prjPath)
	},
}

func init() {
	f := unpackCmd.Flags()
	f.BoolVarP(&flagForce, "force", "f", false, "force overwrite of existing files")
}

var projectsCmd = &cobra.Command{
	Use: "projects",
}

func init() {
	f := convertContainerCmd.Flags()
	f.StringP("user", "u", "", "container registry user")
	f.StringP("password", "p", "", "container registry password")
	f.StringP("config", "c", "", "container registry configuration list")
}

var convertContainerCmd = &cobra.Command{
	Use:   "convert-container REPO:APP DESTFOLDER",
	Args:  cobra.ExactValidArgs(2),
	Short: "Convert containers into vorteil.io virtual machines",
	Long: `Convert containers into vorteil.io project folders. This command can convert
containers from a remote repository as well as from local container runtimes.
At the moment docker and containerd are supported.

Local conversion examples:

vorteil projects convert-container local.docker/nginx /target/directory
vorteil projects convert-container local.containerd/docker.io/library/tomcat:latest /target/directory

Remote conversion examples:

./vorteil projects convert-container --config=/vconvert.yaml nginx /tmp/nginx

The config file provided maps remote repository names to urls. If no file is provided
docker.io, mcr.microsoft.com and gcr.io are automatically added. The following is an example
config yaml:

repositories:
  myrepo:
   url: https://myurl
`,
	Run: func(cmd *cobra.Command, args []string) {

		// in case of an error we pass empty user/pwd/config in
		user, _ := cmd.Flags().GetString("user")
		pwd, _ := cmd.Flags().GetString("password")
		config, _ := cmd.Flags().GetString("config")

		cc, err := vconvert.NewContainerConverter(args[0], config, log)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		err = cc.ConvertToProject(args[1], user, pwd)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
	},
}

var importSharedObjectsCmd = &cobra.Command{
	Use:   "import-shared-objects [PROJECT]",
	Short: "Import shared objects required by the binary targeted within the project.",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var projectPath string = "."
		var err error

		// Get Project Path
		if len(args) != 0 {
			projectPath, err = filepath.Abs(args[0])
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}
		}

		// Set up logging functions
		logWarn := func(format string, a ...interface{}) {
			log.Warnf(format, a...)
		}

		logInfo := func(format string, a ...interface{}) {
			log.Printf(format, a...)
		}

		logDebug := func(format string, a ...interface{}) {
			log.Printf(format, a...)
		}

		// Create Import Operation
		importOperation, err := vproj.NewImportSharedObject(projectPath, vproj.ImportSharedObjectsOptions{
			ExcludeDefaultLibs: flagExcludeDefault,
			LoggerWarn:         logWarn,
			LoggerInfo:         logInfo,
			LoggerDebug:        logDebug,
		})

		if err != nil {
			log.Errorf("%v", err)
			os.Exit(2)
		}

		// Start Import Operation
		if err = importOperation.Start(); err != nil {
			log.Errorf("%v", err)
			os.Exit(3)
		}
	},
}

func init() {
	f := importSharedObjectsCmd.Flags()
	f.BoolVarP(&flagExcludeDefault, "no-defaults", "e", false, "exclude default shared objects")
}

var provisionersCmd = &cobra.Command{
	Use: "provisioners",
}

var provisionersDefaultCmd = &cobra.Command{
	Use: "default",
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
		fmt.Println("TODO")
	},
}

var provisionersDeleteCmd = &cobra.Command{
	Use: "delete",
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
		fmt.Println("TODO")
	},
}

var provisionersDescribeCmd = &cobra.Command{
	Use: "describe",
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
		fmt.Println("TODO")
	},
}

var provisionersExportCmd = &cobra.Command{
	Use: "export",
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
		fmt.Println("TODO")
	},
}

var provisionersImportCmd = &cobra.Command{
	Use: "import",
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
		fmt.Println("TODO")
	},
}

var provisionersListCmd = &cobra.Command{
	Use: "list",
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
		fmt.Println("TODO")
	},
}

var provisionersNewCmd = &cobra.Command{
	Use: "new",
}

var provisionersNewAmazonEC2Cmd = &cobra.Command{
	Use: "amazon-ec2",
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
		fmt.Println("TODO")
	},
}

var provisionersNewAzureCmd = &cobra.Command{
	Use: "azure",
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
		fmt.Println("TODO")
	},
}

var provisionersNewGoogleCmd = &cobra.Command{
	Use: "google",
	Run: func(cmd *cobra.Command, args []string) {
		// Do Stuff Here
		fmt.Println("TODO")
	},
}

var initFirecrackerCmd = &cobra.Command{
	Use:    "firecracker-setup",
	Short:  "Initialize firecracker by spawning a Bridge Device and a DHCP server",
	Long:   `The init firecracker command is a convenience function to quickly setup the bridge device and DHCP server that firecracker will use`,
	Hidden: true,
	Args:   cobra.MaximumNArgs(0),
	Run: func(cmd *cobra.Command, args []string) {
		err := firecracker.SetupBridgeAndDHCPServer()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
	},
}

var runCmd = &cobra.Command{
	Use:   "run [RUNNABLE]",
	Short: "Quick-launch a virtual machine",
	Long: `The run command is a convenience function for quickly getting a Vorteil machine
up and running. It attempts to emulate the behaviour of running the binary
natively as best as possible, which includes making it superficially appear as
though the virtual machine is a child process of the CLI by handling interrupts
and cleaning up the instance when it's done.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		buildablePath := "."
		if len(args) >= 1 {
			buildablePath = args[0]
		}

		pkgBuilder, err := getPackageBuilder("BUILDABLE", buildablePath)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer pkgBuilder.Close()

		err = modifyPackageBuilder(pkgBuilder)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		pkgReader, err := vpkg.ReaderFromBuilder(pkgBuilder)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}
		defer pkgReader.Close()

		pkgReader, err = vpkg.PeekVCFG(pkgReader)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		cfgf := pkgReader.VCFG()
		cfg, err := vcfg.LoadFile(cfgf)
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		err = initKernels()
		if err != nil {
			log.Errorf("%v", err)
			os.Exit(1)
		}

		switch flagPlatform {
		case platformQEMU:
			err = runQEMU(pkgReader, cfg)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}
		case platformVirtualBox:
			err = runVirtualBox(pkgReader, cfg)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}
		case platformHyperV:
			err = runHyperV(pkgReader, cfg)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}
		case platformFirecracker:
			err = runFirecracker(pkgReader, cfg)
			if err != nil {
				log.Errorf("%v", err)
				os.Exit(1)
			}
		default:
			log.Errorf("%v", fmt.Errorf("platform '%s' not supported", flagPlatform).Error())
			os.Exit(1)
		}

	},
}

func init() {
	f := runCmd.Flags()
	f.StringVar(&flagPlatform, "platform", "qemu", "run a virtual machine with appropriate hypervisor (qemu, firecracker, virtualbox, hyper-v)")
	f.BoolVar(&flagGUI, "gui", false, "when running virtual machine show gui of hypervisor")
	f.BoolVar(&flagShell, "shell", false, "add a busybox shell environment to the image")
}
