package flag

import "github.com/spf13/pflag"

// Flag is a datatype-agnostic interface for flag objects
type Flag interface {
	FlagKey() string
	FlagShort() string
	FlagUsage() string
	FlagValidate() error
	AddTo(flagSet *pflag.FlagSet)
	AddUnhiddenTo(flagSet *pflag.FlagSet)
}

// Part object contains important information for any flag type
type Part struct {
	Key    string
	short  string
	usage  string
	hidden bool
}

// NewFlagPart returns a new Part object
func NewFlagPart(key, usage string, hidden bool) Part {
	return Part{
		Key:    key,
		usage:  usage,
		hidden: hidden,
	}
}

// FlagKey returns the flag key
func (p Part) FlagKey() string {
	return p.Key
}

// FlagShort returns the flag 'short' info field
func (p Part) FlagShort() string {
	return p.short
}

// FlagUsage returns the flag 'usage' info field
func (p Part) FlagUsage() string {
	return p.usage
}
