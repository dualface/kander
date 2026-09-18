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
