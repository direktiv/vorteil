package imagetools

import (
	"github.com/vorteil/vorteil/pkg/vdecompiler"
)

// ImageGPTReport ...
type ImageGPTReport struct {
	HeaderLBA       int
	BackupLBA       int
	FirstUsableLBA  int
	LastUsableLBA   int
	FirstEntriesLBA int
	Entries         []GPTEntry
}

// GPTEntry ...
type GPTEntry struct {
	Name     string
	FirstLBA int
	LastLBA  int
}

// ImageGPT ...
func ImageGPT(vorteilImage *vdecompiler.IO) (ImageGPTReport, error) {
	var gptOut ImageGPTReport
	header, err := vorteilImage.GPTHeader()
	if err != nil {
		return gptOut, err
	}

	entries, err := vorteilImage.GPTEntries()
	if err != nil {
		return gptOut, err
	}

	gptOut.HeaderLBA = int(header.CurrentLBA)
	gptOut.BackupLBA = int(header.BackupLBA)
	gptOut.FirstUsableLBA = int(header.FirstUsableLBA)
	gptOut.LastUsableLBA = int(header.LastUsableLBA)
	gptOut.FirstEntriesLBA = int(header.StartLBAParts)

	for _, entry := range entries {
		name := vdecompiler.UTF16toString(entry.Name[:])
		if name != "" {
			gptOut.Entries = append(gptOut.Entries, GPTEntry{
				Name:     name,
				FirstLBA: int(entry.FirstLBA),
				LastLBA:  int(entry.LastLBA),
			})
		}
	}

	return gptOut, err
}
