package flag

import "github.com/spf13/pflag"

// UintFlag handles uint flags
type UintFlag struct {
	FlagPart
	Value    uint
	Validate func(f UintFlag) error
}

// NewUintFlag returns a new UintFlag object
func NewUintFlag(key, usage string, hidden bool, validate func(UintFlag) error) UintFlag {
	return UintFlag{
		FlagPart: NewFlagPart(key, usage, hidden),
		Validate: validate,
	}
}

// AddTo satisfies the Flag interface requirement
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

// AddUnhiddenTo satisfies the Flag interface requirement
func (f *UintFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.UintVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.UintVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
}

// FlagValidate satisfies the Flag interface requirement
func (f UintFlag) FlagValidate() error {
	if f.Validate == nil {
		return nil
	}
	return f.Validate(f)
}
