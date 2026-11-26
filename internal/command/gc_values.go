package command

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// The retention and size limits shared by `kander server gc` and `kander gc`. A shorter
// retention or a smaller kept log could remove what a running operation still needs.
const (
	DefaultRetention = 7 * 24 * time.Hour
	MinRetention     = time.Hour
	DefaultLogKeep   = 64 << 20
	MinLogKeep       = 1 << 20
)

// ParseRetention reads the value of --older-than: a Go duration ("36h", "90m") or a whole
// number of days ("7d"). The result is at least MinRetention; any other value is an argument
// error that names the flag.
func ParseRetention(flag, raw string) (time.Duration, error) {
	var d time.Duration
	if days, ok := strings.CutSuffix(raw, "d"); ok && days != "" && strings.Trim(days, "0123456789") == "" {
		n, err := strconv.ParseInt(days, 10, 64)
		if err != nil || n > int64(math.MaxInt64/int64(24*time.Hour)) {
			return 0, Usage("gc.invalid_retention", "--"+flag, Escape(raw))
		}
		d = time.Duration(n) * 24 * time.Hour
	} else {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return 0, Usage("gc.invalid_retention", "--"+flag, Escape(raw))
		}
		d = parsed
	}
	if d < MinRetention {
		return 0, Usage("gc.invalid_retention", "--"+flag, Escape(raw))
	}
	return d, nil
}

// byteUnits are the binary suffixes ParseByteSize accepts, largest first.
var byteUnits = []struct {
	suffix string
	shift  uint
}{{"GiB", 30}, {"MiB", 20}, {"KiB", 10}}

// ParseByteSize reads the value of --log-keep: a whole number of bytes, or a whole number
// followed by KiB, MiB or GiB. The result is at least MinLogKeep; any other value is an
// argument error that names the flag.
func ParseByteSize(flag, raw string) (int64, error) {
	digits, shift := raw, uint(0)
	for _, unit := range byteUnits {
		if rest, ok := strings.CutSuffix(raw, unit.suffix); ok {
			digits, shift = rest, unit.shift
			break
		}
	}
	if digits == "" || strings.Trim(digits, "0123456789") != "" {
		return 0, Usage("gc.invalid_size", "--"+flag, Escape(raw))
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n > math.MaxInt64>>shift || n<<shift < MinLogKeep {
		return 0, Usage("gc.invalid_size", "--"+flag, Escape(raw))
	}
	return n << shift, nil
}
