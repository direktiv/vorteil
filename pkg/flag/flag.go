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

// FlagPart object contains important information for any flag type
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

// FlagKey returns the flag key
func (p FlagPart) FlagKey() string {
	return p.Key
}

// FlagShort returns the flag 'short' info field
func (p FlagPart) FlagShort() string {
	return p.short
}

// FlagUsage returns the flag 'usage' info field
func (p FlagPart) FlagUsage() string {
	return p.usage
}
