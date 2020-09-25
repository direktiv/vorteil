package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/cmd/vorteil/imageutil"
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

func generateImageCommand(use string, short string, args cobra.PositionalArgs, run func(cmd *cobra.Command, args []string)) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		Run:   run,
	}
}

var decompileCmd = generateImageCommand("decompile IMAGE OUTPUT", "Create a usable project directory from a Vorteil disk vdecompiler.", cobra.ExactArgs(2), func(cmd *cobra.Command, args []string) {
	srcPath := args[0]
	outPath := args[1]
	decompile(srcPath, outPath)
})

func init() {
	f := decompileCmd.Flags()
	f.BoolVarP(&flagTouched, "touched", "t", false, "Only extract files that have been 'touched'.")
}

var catCmd = generateImageCommand("cat IMAGE FILEPATH...", "Concatenate files and print on the standard output.", cobra.MinimumNArgs(2), func(cmd *cobra.Command, args []string) {
	err := imageutil.Cat(args, flagOS)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
})

func init() {
	f := catCmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
}

var cpCmd = generateImageCommand("cp IMAGE SRC_FILEPATH DEST_FILEPATH", "Copy files and directories from an image to your system.", cobra.ExactArgs(3), func(cmd *cobra.Command, args []string) {
	err := imageutil.CP(log, args, flagOS)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
})

func init() {
	f := cpCmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
}

var duCmd = generateImageCommand("du IMAGE [FILEPATH]", "Calculate file space usage.", cobra.RangeArgs(1, 2), func(cmd *cobra.Command, args []string) {
	err := imageutil.DU(log, cmd, args)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
})

func init() {
	f := duCmd.Flags()
	f.BoolP("all", "a", false, "Write counts for all files, not just directories.")
	f.BoolP("free", "f", false, "Add a free-space estimation at the end.")
	f.IntP("max-depth", "l", -1, "Print the total only if the file is within a certain depth.")
	f.StringP("numbers", "n", "short", "Number printing format")
}

var formatCmd = generateImageCommand("format IMAGE", "Image file format information.", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string) {
	err := imageutil.Format(log, args)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
})

var fsCmd = generateImageCommand("fs IMAGE", "Summarize the information in the main file-system's metadata.", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string) {
	err := imageutil.FS(log, cmd, args)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
})

func init() {
	f := fsCmd.Flags()
	f.StringP("numbers", "n", "short", "Number printing format")
}

var fsimgCmd = generateImageCommand("fsimg IMAGE DEST", "Copy the image's file-system partition.", cobra.ExactArgs(2), func(cmd *cobra.Command, args []string) {
	err := imageutil.FSImg(cmd, args)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
})

var gptCmd = generateImageCommand("gpt IMAGE", "Summarize the information in the GUID Partition Table.", cobra.ExactArgs(1), func(cmd *cobra.Command, args []string) {
	err := imageutil.GPT(log, cmd, args)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
})

func init() {
	f := gptCmd.Flags()
	f.StringP("numbers", "n", "short", "Number printing format")
}

var lsCmd = generateImageCommand("ls IMAGE [FILEPATH]", "List directory contents.", cobra.RangeArgs(1, 2), func(cmd *cobra.Command, args []string) {
	err := imageutil.LS(log, cmd, args, flagOS)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
})

func init() {
	f := lsCmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
	f.BoolP("all", "a", false, "Do not ignore entries starting with \".\".")
	f.BoolP("almost-all", "A", false, "Do not list implied \".\" and \"..\".")
	f.BoolP("long", "l", false, "Use a long listing format.")
	f.BoolP("recursive", "R", false, "List subdirectories recursively.")
	f.StringP("numbers", "n", "short", "Number printing format")
}

var md5Cmd = generateImageCommand("md5 IMAGE FILEPATH", "Compute MD5 checksum for a file on an vdecompiler.", cobra.ExactArgs(2), func(cmd *cobra.Command, args []string) {
	err := imageutil.MD5(log, args, flagOS)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
})

func init() {
	f := md5Cmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
	f.StringP("numbers", "n", "short", "Number printing format")
}

var statCmd = generateImageCommand("stat IMAGE [FILEPATH]", "Print detailed metadata relating to the file at FILE_PATH.", cobra.RangeArgs(1, 2), func(cmd *cobra.Command, args []string) {
	err := imageutil.Stat(log, cmd, args, flagOS)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
})

func init() {
	f := statCmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
	f.StringP("numbers", "n", "short", "Number printing format")
}

var treeCmd = generateImageCommand("tree IMAGE [FILEPATH]", "List contents of directories in a tree-like format.", cobra.RangeArgs(1, 2), func(cmd *cobra.Command, args []string) {
	err := imageutil.Tree(log, args, flagOS)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
})

func init() {
	f := treeCmd.Flags()
	f.BoolVarP(&flagOS, "vpartition", "p", false, "Read files from the Vorteil OS partition instead of the file-system partition.")
}
