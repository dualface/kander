package command

import (
	"strings"

	"github.com/spf13/cobra"
)

// Exclusive reports an argument error when more than one of the named flags is given.
func Exclusive(c *cobra.Command, names ...string) error {
	var given []string
	for _, name := range names {
		if c.Flags().Changed(name) {
			given = append(given, "--"+name)
		}
	}
	if len(given) > 1 {
		return Usage("cli.error_flags_exclusive", strings.Join(given, ", "))
	}
	return nil
}

// Together reports an argument error when only some of the named flags are given.
func Together(c *cobra.Command, names ...string) error {
	var given int
	labels := make([]string, len(names))
	for i, name := range names {
		labels[i] = "--" + name
		if c.Flags().Changed(name) {
			given++
		}
	}
	if given != 0 && given != len(names) {
		return Usage("cli.error_flags_together", strings.Join(labels, ", "))
	}
	return nil
}

// Needs reports an argument error when flag is given without the flag it depends on.
func Needs(c *cobra.Command, flag, needed string) error {
	if c.Flags().Changed(flag) && !c.Flags().Changed(needed) {
		return Usage("cli.error_flag_needs", "--"+flag, "--"+needed)
	}
	return nil
}

// First returns the first non-nil error, so a PreRunE can chain several rules.
func First(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// Required reports an argument error when a named flag is missing or given an empty value.
func Required(c *cobra.Command, names ...string) error {
	for _, name := range names {
		switch f := c.Flags().Lookup(name); {
		case !f.Changed:
			return Usage("cli.error_flag_required", "--"+name)
		case f.Value.String() == "":
			return Usage("board.option_requires_a_value", "--"+name)
		}
	}
	return nil
}
