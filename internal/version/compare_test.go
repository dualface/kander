package version

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.10.0", "1.9.9", 1},
		{"v1.2.3", "1.2.3", 0},
		{"0.5.0-3-gabcdef1", "0.5.0", 1},
		{"0.5.0-3-gabcdef1", "0.5.0-9-gabcdef1", -1},
		{"0.5.0-3-gabcdef1", "0.6.0", -1},
		{"1.0.0-rc1", "1.0.0", -1},
		{"1.0.0-rc1", "1.0.0-2-gabcdef1", -1},
		{"2.0.0-alpha.1", "2.0.0-alpha.2", -1},
		{"2.0.0-alpha.2", "2.0.0-alpha.10", -1},
		{"2.0.0-alpha.9999999999999999999999999", "2.0.0-alpha.10", 1},
		{"2.0.0-alpha.1", "2.0.0-alpha.1.x", -1},
		{"2.0.0-alpha.1", "2.0.0-beta.1", -1},
		{"2.0.0-alpha.1", "2.0.0-alpha.x", -1},
		{"2.0.0+build.1", "2.0.0+build.2", 0},
		{"dev", "0.0.1", -1},
		{"0.0.1", "dev", 1},
		{"dev", "dev", 0},
		{"", "dev", -1},
		{"", "", 0},
	}
	for _, tc := range cases {
		if got := Compare(tc.a, tc.b); got != tc.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestValid(t *testing.T) {
	for _, value := range []string{"1.2.3", "v1.2.3", "0.5.0-3-gabcdef1", "1.0.0-rc1"} {
		if !Valid(value) {
			t.Errorf("Valid(%q) = false, want true", value)
		}
	}
	for _, value := range []string{"dev", "", "1.2", "1.2.3-../bad", "1.2.3-", "1.2.3-alpha.01", "01.2.3", "9999999999999999999999999.0.0"} {
		if Valid(value) {
			t.Errorf("Valid(%q) = true, want false", value)
		}
	}
}
