package terminal

import "testing"

func TestPowerShellQuote(t *testing.T) {
	for value, want := range map[string]string{
		"":              "''",
		`C:\a b\中文`:     `'C:\a b\中文'`,
		"O'Brien":       "'O''Brien'",
		"a\u2018b":      "'a\u2018\u2018b'",
		"a\u2019; b":    "'a\u2019\u2019; b'",
		"a\u201ab":      "'a\u201a\u201ab'",
		"a\u201bb":      "'a\u201b\u201bb'",
		"\u2019'\u2019": "'\u2019\u2019''\u2019\u2019'",
	} {
		if got := PowerShellQuote(value); got != want {
			t.Errorf("PowerShellQuote(%q)=%q, want %q", value, got, want)
		}
	}
}
