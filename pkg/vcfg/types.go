package vcfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxArgs   = 32
	maxArgLen = 128
)

//
// Size
//

// Size is a wrapper around int used to easily parse, marshal, and convert
// different equivalent representations of quantities.
type Size int

// Unit constants
const (
	Unit Size = 0x1
	Ki   Size = 0x400
	Mi   Size = 0x100000
	Gi   Size = 0x40000000
)

// String returns a string representation of a Size object.
func (x Size) String() string {
	sign := ""
	if int(x) < 0 {
		sign = "+"
	}

	if s := x.Units(Gi); s > 0 && x.IsAligned(Gi) {
		return fmt.Sprintf("%s%d Gi", sign, s)
	} else if s := x.Units(Mi); s > 0 && x.IsAligned(Mi) {
		return fmt.Sprintf("%s%d Mi", sign, s)
	} else if s := x.Units(Ki); s > 0 && x.IsAligned(Ki) {
		return fmt.Sprintf("%s%d Ki", sign, s)
	}
	if x == 0 {
		return ""
	}
	return fmt.Sprintf("%s%d", sign, x)
}

// MarshalText implements encoding.TextMarshaler.
func (x Size) MarshalText() (text []byte, err error) {
	return []byte(x.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (x *Size) UnmarshalText(text []byte) error {
	var err error
	*x, err = ParseSize(string(text))
	if err != nil {
		return err
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (x Size) MarshalJSON() ([]byte, error) {
	return json.Marshal(x.String())
}

// UnmarshalJSON implements json.Unmarshaler.
func (x *Size) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = strings.Trim(s, "\"")
	var err error
	*x, err = ParseSize(s)
	if err != nil {
		return err
	}
	return nil
}

// ParseSize resolves a string into a Size.
func ParseSize(s string) (Size, error) {

	if s == "" {
		return Size(0), nil
	}

	original := s

	s = strings.TrimSpace(s)
	s = strings.ToLower(s)

	l := len(s)

	sign := Size(1)
	if l > 0 && s[0] == '+' {
		sign = Size(-1)
		s = strings.TrimSpace(s[1:])
		l = len(s)
	}

	var suffix byte
	var suffixes = []string{"k", "ki", "m", "mi", "g", "gi"}
	for _, x := range suffixes {
		if strings.HasSuffix(s, x) {
			suffix = x[0]
			s = s[:l-len(x)]
			s = strings.TrimSpace(s)
			break
		}
	}

	k, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		e, ok := err.(*strconv.NumError)
		if !ok {
			return Size(0), err
		}
		return Size(0), fmt.Errorf("parsing \"%s\": %v", original, e.Err)
	}

	if k < 0 {
		return Size(0), fmt.Errorf("parsing \"%s\": cannot accept negative numbers", original)
	}

	switch suffix {
	case 0:
		return sign * Size(k), nil
	case 'k':
		return sign * Size(k) * Ki, nil
	case 'm':
		return sign * Size(k) * Mi, nil
	case 'g':
		return sign * Size(k) * Gi, nil
	default:
		panic(errors.New("how did we get here?"))
	}

}

// Units returns the number of units the size fills, truncated.
func (x Size) Units(unit Size) int {
	return int(math.Abs(float64(int(x) / int(unit))))
}

// IsAligned returns true if the size is an integer multiple
// of the unit.
func (x Size) IsAligned(unit Size) bool {
	return x%unit == 0
}

// Align increases the size (if necessary) to make it aligned
// to the unit.
func (x *Size) Align(unit Size) {
	*x = ((*x + unit - 1) / unit) * unit
}

// IsDelta returns true if the underlying size is a relative value.
func (x Size) IsDelta() bool {
	return x <= 0
}

// ApplyDelta adds the delta provided to the Size object.
func (x *Size) ApplyDelta(delta Size) {
	if !delta.IsDelta() {
		panic(errors.New("cannot apply non delta Size"))
	}
	*x = *x + Size(delta.Units(1))
}

// Bytes is a wrapper around Size used to easily parse, marshal, and convert
// different equivalent representations of size in bytes. Its only real
// difference compared to Size is in how strings and created and parsed.
type Bytes Size

// Common byte constants
const (
	Byte Bytes = 0x1        // a single byte
	KiB  Bytes = 0x400      // a kibibyte (1024 bytes)
	MiB  Bytes = 0x100000   // a mibibyte (1024 kibibytes)
	GiB  Bytes = 0x40000000 // a gibibyte (1024 mibibytes)
)

// String returns a string representation of a Bytes object.
func (x Bytes) String() string {

	str := Size(x).String()

	if strings.HasSuffix(str, "i") {
		return str + "B"
	}
	return str
}

// MarshalText implements encoding.TextMarshaler. This interface is used by
// toml processing packages based on github.com/BurntSushi/toml.
func (x Bytes) MarshalText() (text []byte, err error) {
	return []byte(x.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. This interface is used by
// toml processing packages based on github.com/BurntSushi/toml.
func (x *Bytes) UnmarshalText(text []byte) error {
	var err error
	*x, err = ParseBytes(string(text))
	if err != nil {
		return err
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (x Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(x.String())
}

// UnmarshalJSON implements json.Unmarshaler.
func (x *Bytes) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = strings.Trim(s, "\"")
	var err error
	*x, err = ParseBytes(s)
	if err != nil {
		return err
	}
	return nil
}

// ParseBytes resolves a string into a Bytes object.
func ParseBytes(s string) (Bytes, error) {

	if s == "" {
		return Bytes(0), nil
	}

	tmp := s

	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.TrimSuffix(s, "b")

	size, err := ParseSize(s)

	if tmp == s {
		min := 1024 * 1024
		if size.Units(1) < min {
			// only a number was provided: assume MiB instead of B.
			size *= Size(min)
		}
	}

	return Bytes(size), err

}

// Units returns the number of units the size fills, truncated.
func (x Bytes) Units(unit Bytes) int {
	return int(math.Abs(float64(int(x) / int(unit))))
}

// IsAligned returns true if the number of bytes is an integer multiple of the
// unit.
func (x Bytes) IsAligned(unit Bytes) bool {
	return x%unit == 0
}

// Align increases the number of bytes if necessary to make it aligned to the
// unit.
func (x *Bytes) Align(unit Bytes) {
	*x = ((*x + unit - 1) / unit) * unit
}

// IsDelta returns true if the underlying size is a relative value.
func (x Bytes) IsDelta() bool {
	return x <= 0
}

// ApplyDelta adds the delta provided to the Bytes object.
func (x *Bytes) ApplyDelta(delta Bytes) {
	if !delta.IsDelta() {
		panic(errors.New("cannot apply non delta Bytes"))
	}
	*x = *x + Bytes(delta.Units(Byte))
}

//
// Zeroth Arg (argv[0])
//

// A ZerothArg is a customized string with checks for
// compatibility with image-building logic.
type ZerothArg string

// UnmarshalText implements encoding.TextUnmarshaler.
func (x *ZerothArg) UnmarshalText(text []byte) error {
	var err error
	*x, err = ZerothArgFromString(string(text))
	return err
}

// UnmarshalJSON implements json.Unmarshaler.
func (x *ZerothArg) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = strings.Trim(s, "\"")
	var err error
	*x, err = ZerothArgFromString(s)
	return err
}

// ZerothArgFromString resolves a string into a ZerothArg.
func ZerothArgFromString(s string) (ZerothArg, error) {

	l := len(s)
	if l > maxArgLen {
		return ZerothArg(""), fmt.Errorf("cannot exceed %d characters", maxArgLen)
	}

	return ZerothArg(s), nil

}

//
// Args
//

// Args is a customized string with checks for compatibility
// with image-building logic.
// type Args string

// // UnmarshalText implements encoding.TextUnmarshaler.
// func (x *Args) UnmarshalText(text []byte) error {
// 	var err error
// 	*x, err = ArgsFromString(string(text))
// 	return err
// }

// // UnmarshalJSON implements json.Unmarshaler.
// func (x *Args) UnmarshalJSON(data []byte) error {
// 	s := string(data)
// 	s = strings.Trim(s, "\"")
// 	var err error
// 	*x, err = ArgsFromString(s)
// 	return err
// }

// // ArgsFromString resolves a string into Args.
// func ArgsFromString(s string) (Args, error) {

// 	_, err := shellwords.Parse(s)
// 	if err != nil {
// 		return Args(""), err
// 	}

// 	return Args(s), nil

// }

// Filesystem instructs the compiler to use a specific filesystem format
type Filesystem string

// Supported filesystem types
var (
	Ext2FS = Filesystem("ext2")
	Ext4FS = Filesystem("ext4")
	XFS    = Filesystem("xfs")
)

//
// URL
//

// URL is a customized string for validating urls unmarshalled
// from json or toml.
type URL string

// UnmarshalText implements encoding.TextUnmarshaler.
func (x *URL) UnmarshalText(text []byte) error {
	var err error
	*x, err = URLFromString(string(text))
	return err
}

// UnmarshalJSON implements json.Unmarshaler.
func (x *URL) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = strings.Trim(s, "\"")
	var err error
	*x, err = URLFromString(s)
	return err
}

// URLFromString resolves a string into Args.
func URLFromString(s string) (URL, error) {

	if s == "" {
		return URL(""), nil
	}

	_, err := url.Parse(s)
	if err != nil {
		return URL(""), err
	}

	return URL(s), nil

}

//
// STDOUT MODE
//

// StdoutMode ..
type StdoutMode int

// ..
const (
	StdoutModeDefault StdoutMode = iota
	StdoutModeStandard
	StdoutModeScreenOnly
	StdoutModeSerialOnly
	StdoutModeDisabled
	StdoutModeUnknown
)

var stdoutModeStrings = [...]string{
	"",
	"standard",
	"screen",
	"serial",
	"disabled",
	"unknown",
}

// String ..
func (x StdoutMode) String() string {
	return stdoutModeStrings[x]
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (x *StdoutMode) UnmarshalText(text []byte) error {
	var err error
	*x = StdoutModeFromString(string(text))
	if *x == StdoutModeUnknown {
		return errors.New("unknown stdout mode")
	}
	return err
}

// MarshalText ..
func (x StdoutMode) MarshalText() (text []byte, err error) {
	return []byte(x.String()), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (x *StdoutMode) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = strings.Trim(s, "\"")
	var err error
	*x = StdoutModeFromString(s)
	if *x == StdoutModeUnknown {
		return errors.New("unknown stdout mode")
	}
	return err
}

// MarshalJSON ..
func (x StdoutMode) MarshalJSON() ([]byte, error) {
	s := x.String()
	return json.Marshal(s)
}

// StdoutModeFromString ..
func StdoutModeFromString(s string) StdoutMode {
	l := len(stdoutModeStrings)

	for i := 0; i < l-1; i++ {
		if stdoutModeStrings[i] == s {
			return StdoutMode(i)
		}
	}

	return StdoutModeUnknown
}

//
// INODES QUOTA
//

// InodesQuota specifies the minimum number of inodes that
// must exist on a compiled file-system.
type InodesQuota int

func (x InodesQuota) validate() error {

	if x < 0 {
		return errors.New("cannot have fewer than zero inodes")
	}

	if x > 0x100000000-1 {
		return errors.New("cannot exceed 4294967295 inodes")
	}

	return nil

}

// TextUnmarshaler implements encoding.TextUnmarshaler.
func (x *InodesQuota) TextUnmarshaler(text []byte) error {
	var k int
	err := json.Unmarshal(text, &k)
	if err != nil {
		return err
	}
	*x = InodesQuota(k)
	return x.validate()
}

// UnmarshalJSON implements json.Unmarshaler.
func (x *InodesQuota) UnmarshalJSON(data []byte) error {
	var k int
	err := json.Unmarshal(data, &k)
	if err != nil {
		return err
	}
	*x = InodesQuota(k)
	return x.validate()
}

//
// DURATION
//

// Duration specifies the a duration of time.
type Duration time.Duration

// Duration ..
func (x Duration) Duration() time.Duration {
	return time.Duration(x)
}

// String ..
func (x Duration) String() string {
	return x.Duration().String()
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (x *Duration) UnmarshalText(text []byte) error {
	var err error
	*x, err = DurationFromString(string(text))
	return err
}

// MarshalText ..
func (x Duration) MarshalText() (text []byte, err error) {
	return []byte(x.String()), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (x *Duration) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = strings.Trim(s, "\"")
	var err error
	*x, err = DurationFromString(s)
	return err
}

// MarshalJSON ..
func (x Duration) MarshalJSON() ([]byte, error) {
	s := x.String()
	return json.Marshal(s)
}

// DurationFromString ..
func DurationFromString(s string) (Duration, error) {

	if s == "" {
		return Duration(0), nil
	}

	i, err := strconv.ParseFloat(s, 64)
	if err == nil {
		return Duration(time.Duration(float64(time.Second) * i)), nil
	}

	d, err := time.ParseDuration(s)
	if err != nil {
		return Duration(0), err
	}

	return Duration(d), nil
}

//
// Timestamp
//

var layouts = []string{
	time.ANSIC,
	time.UnixDate,
	time.RubyDate,
	time.RFC822,
	time.RFC822Z,
	time.RFC850,
	time.RFC1123,
	time.RFC1123Z,
	time.RFC3339,
	time.RFC3339Nano,
	time.Stamp,
	"2006 01 02",
	"2006-01-02",
	"2006/01/02",
	"2006.01.02",
	"02 01 2006",
	"02-01-2006",
	"02/01/2006",
	"02.01.2006",
	"02 01 06",
	"02-01-06",
	"02/01/06",
	"02.01.06",
}

// Timestamp specifies the a date and time.
type Timestamp struct {
	timestamp time.Time
	layout    string
}

// Time ..
func (x Timestamp) Time() time.Time {
	return x.timestamp
}

// Unix ..
func (x Timestamp) Unix() int64 {
	return x.timestamp.Unix()
}

func (x Timestamp) String() string {
	if x.layout == "" {
		return ""
	}
	if x.layout == "epoch" {
		return fmt.Sprintf("%d", x.timestamp.Unix())
	}
	return x.timestamp.Format(x.layout)
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (x *Timestamp) UnmarshalText(text []byte) error {
	var err error
	*x, err = TimestampFromString(string(text))
	return err
}

// MarshalText ..
func (x Timestamp) MarshalText() (text []byte, err error) {
	return []byte(x.String()), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (x *Timestamp) UnmarshalJSON(data []byte) error {
	s := string(data)
	s = strings.Trim(s, "\"")
	var err error
	*x, err = TimestampFromString(s)
	return err
}

// MarshalJSON ..
func (x Timestamp) MarshalJSON() ([]byte, error) {
	s := x.String()
	return json.Marshal(s)
}

// TimestampFromTime ..
func TimestampFromTime(t time.Time) Timestamp {
	return Timestamp{
		timestamp: t,
		layout:    time.RFC822,
	}
}

// TimestampFromString ..
func TimestampFromString(s string) (Timestamp, error) {

	if s == "" {
		return Timestamp{
			timestamp: time.Time{},
			layout:    "",
		}, nil
	}

	v, err := strconv.ParseUint(s, 10, 64)
	if err == nil {
		t := time.Unix(int64(v), 0)
		return Timestamp{
			timestamp: t,
			layout:    "epoch",
		}, nil
	}

	l := len(layouts)
	for i := 0; i < l; i++ {
		layout := layouts[i]
		t, err := time.Parse(layout, s)
		if err != nil {
			continue
		}
		return Timestamp{
			timestamp: t,
			layout:    layout,
		}, nil
	}

	return Timestamp{}, fmt.Errorf("unrecognized timestamp format: %s", s)
}
