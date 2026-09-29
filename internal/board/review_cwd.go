package board

import (
	"path"
	"runtime"
	"strings"
)

// SameReviewCWD reports whether two review evidence CWDs name the same directory. The comparison is
// lexical and never touches the filesystem, so it also accepts spellings stored by earlier plans and
// closures without rewriting them. On Windows separators are interchangeable, a trailing separator is
// ignored, and case is folded; elsewhere the cleaned paths must match exactly.
func SameReviewCWD(a, b string) bool {
	return sameReviewCWD(a, b, runtime.GOOS == "windows")
}

func sameReviewCWD(a, b string, windows bool) bool {
	if a == "" || b == "" {
		return a == b
	}
	if !windows {
		return path.Clean(a) == path.Clean(b)
	}
	return windowsReviewCWDKey(a) == windowsReviewCWDKey(b)
}

// windowsReviewCWDKey folds one Windows path spelling into a comparison key: forward slashes, cleaned
// dot segments and repeated separators, no trailing separator, and lower case. A leading `//` of a UNC
// path is kept so that a UNC path never matches a drive-relative rooted path.
func windowsReviewCWDKey(p string) string {
	s := strings.ReplaceAll(p, `\`, "/")
	unc := strings.HasPrefix(s, "//")
	s = path.Clean(s)
	if unc && !strings.HasPrefix(s, "//") {
		s = "/" + s
	}
	return strings.ToLower(s)
}
