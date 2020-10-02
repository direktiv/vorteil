package flag

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"fmt"

	"github.com/spf13/pflag"
)

// FlagsList contains an array of Flag objects
type FlagsList []Flag

// AddTo satisfies the Flag interface requirement
func (f FlagsList) AddTo(flagSet *pflag.FlagSet) {
	for _, x := range f {
		x.AddTo(flagSet)
	}
}

// AddUnhiddenTo satisfies the Flag interface requirement
func (f FlagsList) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	for _, x := range f {
		x.AddUnhiddenTo(flagSet)
	}
}

// Validate satisfies the Flag interface requirement
func (f FlagsList) Validate() error {
	for _, x := range f {
		err := x.FlagValidate()
		if err != nil {
			fmt.Println(x.FlagKey())
			return err
		}
	}
	return nil
}
