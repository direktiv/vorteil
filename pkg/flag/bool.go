package flag

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

// BoolFlag handles boolean flags
type BoolFlag struct {
	Part
	Value    bool
	Validate func(Value BoolFlag) error
}

// AddTo satisfies the Flag interface requirement
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

// AddUnhiddenTo satisfies the Flag interface requirement
func (f *BoolFlag) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	if f.short == "" {
		flagSet.BoolVar(&f.Value, f.Key, f.Value, f.usage)
	} else {
		flagSet.BoolVarP(&f.Value, f.Key, f.short, f.Value, f.usage)
	}
}

// FlagValidate satisfies the Flag interface requirement
func (f BoolFlag) FlagValidate() error {
	if f.Validate == nil {
		return nil
	}
	return f.Validate(f)
}

// NBoolFlag handle bool flags in cases with a varying number of possible occurrences of the flag (ie --flag[x].my-bool)
type NBoolFlag struct {
	Part
	Total    *int
	void     bool
	Value    []bool
	Validate func(f NBoolFlag) error
}

// NewNBoolFlag returns a new NBoolFlag object
func NewNBoolFlag(key, usage string, total *int, hidden bool, validate func(NBoolFlag) error) NBoolFlag {
	return NBoolFlag{
		Part:     NewFlagPart(key, usage, hidden),
		Total:    total,
		Validate: validate,
	}
}

// AddTo satisfies the Flag interface requirement
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

// AddUnhiddenTo satisfies the Flag interface requirement
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

// FlagValidate satisfies the Flag interface requirement
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
