package api

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// DeliveryEvidence records an authenticated holder's successful push and exact
// remote-ref observation. The server stores these facts; it never runs Git.
type DeliveryEvidence struct {
	TaskID       string    `json:"task_id"`
	ExecutionID  string    `json:"execution_id"`
	Generation   uint64    `json:"generation"`
	CardRevision uint64    `json:"card_revision"`
	Commit       string    `json:"commit"`
	Ref          string    `json:"ref"`
	Remote       string    `json:"remote"`
	ObservedAt   time.Time `json:"observed_at"`
}

func CommitSHA(value string) bool {
	return len(value) == 40 && strings.Trim(value, "0123456789abcdef") == ""
}

// DeliveryRef is immutable per commit and ownership generation. Late pushes
// from an expired execution cannot move the successor's delivery reference.
func DeliveryRef(project, task string, generation uint64, commit string) string {
	return fmt.Sprintf("refs/heads/kander/%s/%s/g%d/%s", project, task, generation, commit)
}

// SafeGitRemote prevents credentials from entering shared project evidence.
// SSH's conventional user name is allowed; passwords and HTTP userinfo are not.
func SafeGitRemote(remote string) bool {
	if remote == "" || strings.TrimSpace(remote) != remote || strings.ContainsAny(remote, "\x00\r\n") || strings.HasPrefix(remote, "-") || strings.Contains(remote, "::") {
		return false
	}
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil || u.RawQuery != "" || u.Fragment != "" {
			return false
		}
		switch u.Scheme {
		case "https", "http", "ssh", "git", "file":
		default:
			return false
		}
		if u.User != nil {
			if _, password := u.User.Password(); password || u.Scheme != "ssh" {
				return false
			}
		}
	} else if before, _, hasUser := strings.Cut(remote, "@"); hasUser && strings.Contains(before, ":") {
		return false
	}
	return true
}
