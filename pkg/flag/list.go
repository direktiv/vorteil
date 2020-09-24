package flag

import (
	"fmt"

	"github.com/spf13/pflag"
)

type FlagsList []Flag

func (f FlagsList) AddTo(flagSet *pflag.FlagSet) {
	for _, x := range f {
		x.AddTo(flagSet)
	}
}

func (f FlagsList) AddUnhiddenTo(flagSet *pflag.FlagSet) {
	for _, x := range f {
		x.AddUnhiddenTo(flagSet)
	}
}

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
