package flag

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

// NStringFlag handle string flags in cases with a varying number of possible occurrences of the flag (ie --flag[x].my-string="test")
type NStringFlag struct {
	Part
	Total    *int
	void     string
	Value    []string
	Validate func(f NStringFlag) error
}

// NewNStringFlag creates a new NStringFlag object
func NewNStringFlag(key, usage string, total *int, hidden bool, validate func(NStringFlag) error) NStringFlag {
	return NStringFlag{
		Part:     NewFlagPart(key, usage, hidden),
		Total:    total,
		Validate: validate,
	}
}

// AddTo satisfies the Flag interface requirement
func (f *NStringFlag) AddTo(flagSet *pflag.FlagSet) {
	if f.Value == nil {
		f.Value = make([]string, *f.Total, *f.Total)
	}

	key := strings.Replace(f.Key, "<<N>>", "i", -1)
	flagSet.StringVar(&f.void, key, f.void, f.usage)
	if f.hidden {
		flag := flagSet.Lookup(key)
		flag.Hidden = true
	}
	for i := 0; i < *f.Total; i++ {
		key = f.nKey(i)
		flagSet.StringVar(&f.Value[i], key, f.Value[i], f.usage)
		flagSet.MarkHidden(key)
		if f.hidden {
			flag := flagSet.Lookup(key)
			flag.Hidden = true
		}
	}

}

// AddUnhiddenTo satisfies the Flag interface requirement
func (f *NStringFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {

	if f.Value == nil {
		f.Value = make([]string, *f.Total, *f.Total)
	}

	key := strings.Replace(f.Key, "<<N>>", "i", -1)
	flagSet.StringVar(&f.void, key, f.void, f.usage)

	for i := 0; i < *f.Total; i++ {
		key = f.nKey(i)
		flagSet.StringVar(&f.Value[i], key, f.Value[i], f.usage)
		flagSet.MarkHidden(key)
	}

}

// FlagValidate satisfies the Flag interface requirement
func (f NStringFlag) FlagValidate() error {

	if f.void != "" {
		key := strings.Replace(f.Key, "<<N>>", "i", -1)
		suggest := strings.Replace(f.Key, "<<N>>", "0", -1)
		return fmt.Errorf("unknown flag: --%s (substitute 'N' for 0-%d, e.g. %s)", key, f.Total, suggest)
	}

	if f.Validate == nil {
		return nil
	}

	return f.Validate(f)

}

func (f NStringFlag) nKey(n int) string {
	return strings.Replace(f.Key, "<<N>>", fmt.Sprintf("%d", n), -1)
}

// NStringSliceFlag handle string slice flags in cases with a varying number of occurrences of the repeatable flag
type NStringSliceFlag struct {
	Part
	Total    *int
	void     []string
	Value    [][]string
	Validate func(f NStringSliceFlag) error
}

// NewNStringSliceFlag creates a new NStringSliceFlag object
func NewNStringSliceFlag(key, usage string, total *int, hidden bool, validate func(NStringSliceFlag) error) NStringSliceFlag {
	return NStringSliceFlag{
		Part:     NewFlagPart(key, usage, hidden),
		Total:    total,
		Validate: validate,
	}
}

// AddTo satisfies the Flag interface requirement
func (f *NStringSliceFlag) AddTo(flagSet *pflag.FlagSet) {

	if f.Value == nil {
		f.Value = make([][]string, *f.Total, *f.Total)
	}

	key := strings.Replace(f.Key, "<<N>>", "i", -1)
	flagSet.StringSliceVar(&f.void, key, f.void, f.usage)
	if f.hidden {
		flag := flagSet.Lookup(key)
		flag.Hidden = true
	}

	for i := 0; i < *f.Total; i++ {
		key = f.nKey(i)
		flagSet.StringSliceVar(&f.Value[i], key, f.Value[i], f.usage)
		flagSet.MarkHidden(key)
		if f.hidden {
			flag := flagSet.Lookup(key)
			flag.Hidden = true
		}
	}

}

// AddUnhiddenTo satisfies the Flag interface requirement
func (f *NStringSliceFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {

	if f.Value == nil {
		f.Value = make([][]string, *f.Total, *f.Total)
	}

	key := strings.Replace(f.Key, "<<N>>", "i", -1)
	flagSet.StringSliceVar(&f.void, key, f.void, f.usage)

	for i := 0; i < *f.Total; i++ {
		key = f.nKey(i)
		flagSet.StringSliceVar(&f.Value[i], key, f.Value[i], f.usage)
		flagSet.MarkHidden(key)
	}

}

// FlagValidate satisfies the Flag interface requirement
func (f NStringSliceFlag) FlagValidate() error {

	if len(f.void) != 0 {
		key := strings.Replace(f.Key, "<<N>>", "i", -1)
		suggest := strings.Replace(f.Key, "<<N>>", "0", -1)
		return fmt.Errorf("unknown flag: --%s (substitute 'N' for 0-%d, e.g. %s)", key, f.Total, suggest)
	}

	if f.Validate == nil {
		return nil
	}

	return f.Validate(f)

}

func (f NStringSliceFlag) nKey(n int) string {
	return strings.Replace(f.Key, "<<N>>", fmt.Sprintf("%d", n), -1)
}

// StringFlag handles string flags
type StringFlag struct {
	Part
	Value    string
	Validate func(Value StringFlag) error
}

// NewStringFlag creates a new StringFlag object
func NewStringFlag(key, usage string, hidden bool, validate func(StringFlag) error) StringFlag {
	return StringFlag{
		Part:     NewFlagPart(key, usage, hidden),
		Validate: validate,
	}
}

// AddTo satisfies the Flag interface requirement
func (f *StringFlag) AddTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.StringVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.StringVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
	if f.hidden {
		flag := flagSet.Lookup(f.Key)
		flag.Hidden = true
	}
}

// AddUnhiddenTo satisfies the Flag interface requirement
func (f *StringFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.StringVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.StringVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
}

// FlagValidate satisfies the Flag interface requirement
func (f StringFlag) FlagValidate() error {
	if f.Validate == nil {
		return nil
	}
	return f.Validate(f)
}

// StringSliceFlag handles repeatable string flags
type StringSliceFlag struct {
	Part
	Value    []string
	Validate func(f StringSliceFlag) error
}

// NewStringSliceFlag creates a new StringSliceFlag object
func NewStringSliceFlag(key, usage string, hidden bool, validate func(StringSliceFlag) error) StringSliceFlag {
	return StringSliceFlag{
		Part:     NewFlagPart(key, usage, hidden),
		Validate: validate,
	}
}

// AddTo satisfies the Flag interface requirement
func (f *StringSliceFlag) AddTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.StringSliceVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.StringSliceVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
	if f.hidden {
		flag := flagSet.Lookup(f.Key)
		flag.Hidden = true
	}
}

// AddUnhiddenTo satisfies the Flag interface requirement
func (f *StringSliceFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.StringSliceVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.StringSliceVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
}

// FlagValidate satisfies the Flag interface requirement
func (f StringSliceFlag) FlagValidate() error {
	if f.Validate == nil {
		return nil
	}
	return f.Validate(f)
}
