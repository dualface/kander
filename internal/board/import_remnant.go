package board

import (
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
)

// atomicRemnantName is the temporary sibling an atomic write of kander
// creates on POSIX and on Windows alike: "." + name + "." + pid + "." +
// UnixNano + ".tmp". The write renames it over name; an interrupted write
// leaves it behind.
var atomicRemnantName = regexp.MustCompile(`^\.(.+)\.([1-9][0-9]{0,9})\.([1-9][0-9]{0,18})\.tmp$`)

// ImportAtomicRemnant reports whether rel, the slash-separated board path
// of a regular file, is the leftover of an interrupted atomic write, and
// returns the board path of the file the write was replacing. Only the
// .kander subtree and card directories hold such writes; a matching name
// anywhere else, such as the board root or a state directory itself, is an
// ordinary file. The caller passes only regular files.
func ImportAtomicRemnant(rel string) (target string, ok bool) {
	parts := strings.Split(rel, "/")
	if len(parts) < 2 {
		return "", false
	}
	inRecords := strings.EqualFold(parts[0], ".kander")
	inCard := len(parts) >= 3 && slices.Contains(States, parts[0]) && taskIDRe.MatchString(parts[1])
	if !inRecords && !inCard {
		return "", false
	}
	match := atomicRemnantName.FindStringSubmatch(parts[len(parts)-1])
	if match == nil || !validRemnantTarget(match[1]) {
		return "", false
	}
	return path.Join(path.Join(parts[:len(parts)-1]...), match[1]), true
}

// validRemnantTarget accepts one ordinary path component.
func validRemnantTarget(name string) bool {
	return fs.ValidPath(name) && !strings.Contains(name, "/") && name != "."
}
