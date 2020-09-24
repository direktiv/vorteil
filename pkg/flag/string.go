package flag

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

type NStringFlag struct {
	FlagPart
	Total    *int
	void     string
	Value    []string
	Validate func(f NStringFlag) error
}

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

type NStringSliceFlag struct {
	FlagPart
	Total    *int
	void     []string
	Value    [][]string
	Validate func(f NStringSliceFlag) error
}

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

type StringFlag struct {
	FlagPart
	Value    string
	Validate func(Value StringFlag) error
}

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

func (f *StringFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.StringVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.StringVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
}

func (f StringFlag) FlagValidate() error {
	if f.Validate == nil {
		return nil
	}
	return f.Validate(f)
}

type StringSliceFlag struct {
	FlagPart
	Value    []string
	Validate func(f StringSliceFlag) error
}

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

func (f *StringSliceFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.StringSliceVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.StringSliceVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
}

func (f StringSliceFlag) FlagValidate() error {
	if f.Validate == nil {
		return nil
	}
	return f.Validate(f)
}
