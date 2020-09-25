package imageutil

import (
	"github.com/spf13/cobra"
	"github.com/vorteil/vorteil/pkg/elog"
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

func GPT(log elog.View, cmd *cobra.Command, args []string) error {
	numbers, err := cmd.Flags().GetString("numbers")
	if err != nil {
		return err
	}

	err = SetNumbersMode(numbers)
	if err != nil {
		return err
	}

	img := args[0]

	iio, err := vdecompiler.Open(img)
	if err != nil {
		return err
	}
	defer iio.Close()

	header, err := iio.GPTHeader()
	if err != nil {
		return err
	}

	entries, err := iio.GPTEntries()
	if err != nil {
		return err
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
	return nil
}
