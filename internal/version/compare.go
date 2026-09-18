package version

import (
	"regexp"
	"strconv"
	"strings"
)

// releaseRE parses an optional leading v, a semver base, and an optional suffix.
// A `-N-g<hex>` suffix is the git-describe commits-ahead form; any other suffix
// marks a prerelease, which sorts below the plain release of the same base.
var releaseRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-(.*))?$`)

var describeRE = regexp.MustCompile(`^(\d+)-g[0-9a-fA-F]+$`)

type parsedVersion struct {
	major, minor, patch int
	// rank: 0 prerelease, 1 release, 2 commits ahead of the tag
	rank  int
	ahead int
	ok    bool
}

func parseVersion(value string) parsedVersion {
	match := releaseRE.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return parsedVersion{}
	}
	parsed := parsedVersion{
		major: mustAtoi(match[1]),
		minor: mustAtoi(match[2]),
		patch: mustAtoi(match[3]),
		rank:  1,
		ok:    true,
	}
	switch suffix := match[4]; {
	case suffix == "":
	case describeRE.MatchString(suffix):
		parsed.rank = 2
		parsed.ahead = mustAtoi(describeRE.FindStringSubmatch(suffix)[1])
	default:
		parsed.rank = 0
	}
	return parsed
}

func mustAtoi(text string) int {
	value, _ := strconv.Atoi(text)
	return value
}

// Valid reports whether value parses as a release version. Unparseable values
// such as "dev" are not ordered against releases.
func Valid(value string) bool {
	return parseVersion(value).ok
}

// Compare orders two version strings: semver base first, then commits-ahead of
// the tag, then release before prerelease of the same base. Unparseable values
// such as "dev" sort below every parseable version and compare lexically
// against each other.
func Compare(a, b string) int {
	pa, pb := parseVersion(a), parseVersion(b)
	if !pa.ok || !pb.ok {
		switch {
		case pa.ok:
			return 1
		case pb.ok:
			return -1
		}
		return strings.Compare(a, b)
	}
	for _, pair := range [][2]int{
		{pa.major, pb.major},
		{pa.minor, pb.minor},
		{pa.patch, pb.patch},
		{pa.rank, pb.rank},
		{pa.ahead, pb.ahead},
	} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}
