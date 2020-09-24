package flag

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

type BoolFlag struct {
	FlagPart
	Value    bool
	Validate func(Value BoolFlag) error
}

func (f *BoolFlag) AddTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.BoolVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.BoolVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
	if f.hidden {
		flag := flagSet.Lookup(f.Key)
		flag.Hidden = true
	}
}

func (f *BoolFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.BoolVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.BoolVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
}

func (f BoolFlag) FlagValidate() error {
	if f.Validate == nil {
		return nil
	}
	return f.Validate(f)
}

type NBoolFlag struct {
	FlagPart
	Total    *int
	void     bool
	Value    []bool
	Validate func(f NBoolFlag) error
}

func (f *NBoolFlag) AddTo(flagSet *pflag.FlagSet) {

	if f.Value == nil {
		f.Value = make([]bool, *f.Total, *f.Total)
	}

	key := strings.Replace(f.Key, "<<N>>", "i", -1)
	flagSet.BoolVar(&f.void, key, f.void, f.usage)
	if f.hidden {
		flag := flagSet.Lookup(key)
		flag.Hidden = true
	}

	for i := 0; i < *f.Total; i++ {
		key = f.nKey(i)
		flagSet.BoolVar(&f.Value[i], key, f.Value[i], f.usage)
		flagSet.MarkHidden(key)
		if f.hidden {
			flag := flagSet.Lookup(key)
			flag.Hidden = true
		}
	}

}

func (f NBoolFlag) nKey(n int) string {
	return strings.Replace(f.Key, "<<N>>", fmt.Sprintf("%d", n), -1)
}

func (f *NBoolFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {

	if f.Value == nil {
		f.Value = make([]bool, *f.Total, *f.Total)
	}

	key := strings.Replace(f.Key, "<<N>>", "i", -1)
	flagSet.BoolVar(&f.void, key, f.void, f.usage)

	for i := 0; i < *f.Total; i++ {
		key = f.nKey(i)
		flagSet.BoolVar(&f.Value[i], key, f.Value[i], f.usage)
		flagSet.MarkHidden(key)
	}

}

func (f NBoolFlag) FlagValidate() error {

	if f.void {
		key := strings.Replace(f.Key, "<<N>>", "i", -1)
		suggest := strings.Replace(f.Key, "<<N>>", "0", -1)
		return fmt.Errorf("unknown flag: --%s (substitute 'N' for 0-%d, e.g. %s)", key, f.Total, suggest)
	}

	if f.Validate == nil {
		return nil
	}

	return f.Validate(f)

}
