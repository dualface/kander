package command

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// flagError turns a pflag parse error into the localized argument error. A value type that
// rejects its input returns a UsageError from Set; pflag wraps it, so it is unwrapped here.
func flagError(_ *cobra.Command, err error) error {
	var usage *UsageError
	if errors.As(err, &usage) {
		return usage
	}
	var missing *pflag.ValueRequiredError
	var unknown *pflag.NotExistError
	var invalid *pflag.InvalidValueError
	switch {
	case errors.As(err, &unknown):
		return Usage("board.unknown_option", flagLabel(unknown.GetSpecifiedName(), unknown.GetSpecifiedShortnames()))
	case errors.As(err, &missing):
		return Usage("board.option_requires_a_value", flagLabel(missing.GetSpecifiedName(), missing.GetSpecifiedShortnames()))
	case errors.As(err, &invalid):
		return Usage("cli.error_invalid_value", "--"+invalid.GetFlag().Name, Escape(invalid.GetValue()))
	}
	return UsageText(Escape(err.Error()))
}

func flagLabel(name, shorthands string) string {
	if shorthands != "" {
		return "-" + Escape(name)
	}
	return "--" + Escape(name)
}

// Escape renders the control characters of text a user supplied as visible escapes, so that an
// argument echoed in an error cannot move the cursor or recolor the terminal.
func Escape(text string) string {
	var b strings.Builder
	for _, r := range text {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x80 && unicode.IsControl(r):
			fmt.Fprintf(&b, `\x%02x`, r)
		case unicode.IsControl(r):
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// stringValue is a string flag value that can reject what it is given.
type stringValue struct {
	dst   *string
	set   bool
	once  string
	valid func(string) error
}

func (v *stringValue) String() string {
	if v.dst == nil {
		return ""
	}
	return *v.dst
}

func (v *stringValue) Type() string { return "string" }

func (v *stringValue) Set(raw string) error {
	if v.once != "" && v.set {
		return Usage("cli.error_duplicate_option", v.once)
	}
	if v.valid != nil {
		if err := v.valid(raw); err != nil {
			return err
		}
	}
	*v.dst, v.set = raw, true
	return nil
}

// Once registers a string flag that may be given at most once.
func Once(flags *pflag.FlagSet, dst *string, name, usage string) {
	flags.Var(&stringValue{dst: dst, once: "--" + name}, name, usage)
}

// OnceNonEmpty registers a string flag that may be given at most once and never empty.
func OnceNonEmpty(flags *pflag.FlagSet, dst *string, name, usage string) {
	flags.Var(&stringValue{dst: dst, once: "--" + name, valid: func(raw string) error {
		if raw == "" {
			return Usage("board.option_requires_a_value", "--"+name)
		}
		return nil
	}}, name, usage)
}

// Checked registers a string flag whose value must satisfy valid; valid returns the argument
// error to report, and the flag may be repeated with the last value winning.
func Checked(flags *pflag.FlagSet, dst *string, name, usage string, valid func(string) error) {
	flags.Var(&stringValue{dst: dst, valid: valid}, name, usage)
}

// Float registers a number flag. id is the localized text reported for a value that is not a
// number or that valid rejects.
func Float(flags *pflag.FlagSet, dst *float64, name string, def float64, valid func(float64) bool, usage, id string) {
	*dst = def
	flags.Var(&floatValue{dst: dst, valid: valid, id: id}, name, usage)
}

// Seconds registers a flag that takes a positive, finite number of seconds.
func Seconds(flags *pflag.FlagSet, dst *float64, name string, def float64, usage, id string) {
	Float(flags, dst, name, def, func(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) && n > 0 }, usage, id)
}

type floatValue struct {
	dst   *float64
	valid func(float64) bool
	id    string
}

func (v *floatValue) String() string { return strconv.FormatFloat(*v.dst, 'g', -1, 64) }

func (v *floatValue) Type() string { return "number" }

func (v *floatValue) Set(raw string) error {
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || !v.valid(n) {
		return Usage(v.id)
	}
	*v.dst = n
	return nil
}
