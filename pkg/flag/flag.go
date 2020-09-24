package flag

import "github.com/spf13/pflag"

type Flag interface {
	FlagKey() string
	FlagShort() string
	FlagUsage() string
	FlagValidate() error
	AddTo(flagSet *pflag.FlagSet)
	AddUnhiddenTo(flagSet *pflag.FlagSet)
}

type FlagPart struct {
	Key    string
	short  string
	usage  string
	hidden bool
}

// NewFlagPart returns a new FlagPart object
func NewFlagPart(key, usage string, hidden bool) FlagPart {
	return FlagPart{
		Key:    key,
		usage:  usage,
		hidden: hidden,
	}
}

func (p FlagPart) FlagKey() string {
	return p.Key
}

func (p FlagPart) FlagShort() string {
	return p.short
}

func (p FlagPart) FlagUsage() string {
	return p.usage
}
