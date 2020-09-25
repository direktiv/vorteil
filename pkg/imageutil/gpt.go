package imageutil

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// GptCMD summarizes the information in the GUID Partition Table
var GptCMD = &cobra.Command{
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
