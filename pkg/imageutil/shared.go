package imageutil

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/sisatech/tablewriter"
	"github.com/vorteil/vorteil/pkg/elog"
)

var log elog.View

func AttachLogger(logger elog.View) {
	log = logger
}

// PrintableSize is a wrapper around int to alter its string formatting behaviour.
type PrintableSize int

// String returns a string representation of the PrintableSize, formatted according to the global NumbersMode.
func (c PrintableSize) String() string {
	switch NumbersMode {
	case 0:
		x := int(c)
		if x == 0 {
			return "0"
		}
		var units int
		var suffixes = []string{"", "K", "M", "G"}
		for {
			if x%1024 != 0 {
				break
			}
			x /= 1024
			units++
			if units == len(suffixes)-1 {
				break
			}
		}
		return fmt.Sprintf("%d%s", x, suffixes[units])
	case 1:
		return fmt.Sprintf("%d", int(c))
	case 2:
		return fmt.Sprintf("%#x", int(c))
	default:
		panic("invalid NumbersMode")
	}
}

// PlainTable prints data in a grid, handling alignment automatically.
func PlainTable(vals [][]string) {
	if len(vals) == 0 {
		panic(errors.New("no rows provided"))
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetBorder(false)
	table.SetColumnSeparator("")
	for i := 1; i < len(vals); i++ {
		table.Append(vals[i])
	}

	table.Render()
}

// NumbersMode determines which numbers format a PrintableSize should render to.
var NumbersMode int

// SetNumbersMode parses s and sets NumbersMode accordingly.
func SetNumbersMode(s string) error {
	s = strings.ToLower(s)
	s = strings.TrimSpace(s)
	switch s {
	case "", "short":
		NumbersMode = 0
	case "dec", "decimal":
		NumbersMode = 1
	case "hex", "hexadecimal":
		NumbersMode = 2
	default:
		return fmt.Errorf("numbers mode must be one of 'dec', 'hex', or 'short'")
	}
	return nil
}
