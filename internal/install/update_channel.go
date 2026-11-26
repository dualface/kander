package install

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/version"
)

// updateChannel is the release line a running binary follows. Each 2.x
// binary updates only within its own channel: a prerelease never moves to a
// release, a release never moves to a prerelease, and neither moves to 1.x.
type updateChannel int

const (
	channelNone updateChannel = iota
	channelRelease
	channelPrerelease
)

var (
	releaseChannelRE    = regexp.MustCompile(`^2\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	prereleaseChannelRE = regexp.MustCompile(`^2\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-(alpha|beta|rc)\.(0|[1-9][0-9]*)$`)
)

// channelOf classifies a version with an optional leading v. Development
// builds, git-describe builds, build metadata, and every major other than 2
// belong to no channel and therefore never update themselves.
func channelOf(value string) updateChannel {
	value = strings.TrimPrefix(value, "v")
	switch {
	case releaseChannelRE.MatchString(value):
		return channelRelease
	case prereleaseChannelRE.MatchString(value):
		return channelPrerelease
	default:
		return channelNone
	}
}

// releaseEntry is the part of one GitHub releases list item the check reads.
type releaseEntry struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// newestInChannel decodes one releases list page and returns the highest
// version newer than current in current's channel, or "" when none is. A
// candidate must be published, carry a tag of the same channel, and have a
// GitHub prerelease flag that agrees with that channel.
func newestInChannel(body []byte, current string) (string, error) {
	channel := channelOf(current)
	if channel == channelNone {
		return "", nil
	}
	var releases []releaseEntry
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("decode release list: %w", err)
	}
	best := ""
	for _, release := range releases {
		target := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
		if release.Draft || channelOf(target) != channel || release.Prerelease != (channel == channelPrerelease) {
			continue
		}
		if version.Compare(target, current) <= 0 {
			continue
		}
		if best == "" || version.Compare(target, best) > 0 {
			best = target
		}
	}
	return best, nil
}
