package flag

import "github.com/spf13/pflag"

type UintFlag struct {
	FlagPart
	Value    uint
	Validate func(f UintFlag) error
}

func (f *UintFlag) AddTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.UintVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.UintVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
	if f.hidden {
		flag := flagSet.Lookup(f.Key)
		flag.Hidden = true
	}
}

func (f *UintFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.UintVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.UintVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
}

func (f UintFlag) FlagValidate() error {
	if f.Validate == nil {
		return nil
	}
	return f.Validate(f)
}
