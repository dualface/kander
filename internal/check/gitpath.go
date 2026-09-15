package check

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// GitPath is a Git-relative path preserved as raw bytes and encoded as a
// UTF-8/base64 union for JSON.
type GitPath struct {
	Raw []byte `json:"-"`
}

func gitPath(raw []byte) GitPath {
	return GitPath{Raw: bytes.Clone(raw)}
}

func gitPathPtr(raw []byte) *GitPath {
	if raw == nil {
		return nil
	}
	path := gitPath(raw)
	return &path
}

func (p GitPath) MarshalJSON() ([]byte, error) {
	if p.Raw == nil {
		return []byte("null"), nil
	}
	var payload struct {
		UTF8   *string `json:"utf8"`
		Base64 *string `json:"base64"`
	}
	if utf8.Valid(p.Raw) {
		text := string(p.Raw)
		payload.UTF8 = &text
	} else {
		encoded := base64.StdEncoding.EncodeToString(p.Raw)
		payload.Base64 = &encoded
	}
	return json.Marshal(payload)
}

func (p *GitPath) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		p.Raw = nil
		return nil
	}
	var payload struct {
		UTF8   *string `json:"utf8"`
		Base64 *string `json:"base64"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	switch {
	case payload.UTF8 != nil && payload.Base64 == nil:
		p.Raw = []byte(*payload.UTF8)
	case payload.UTF8 == nil && payload.Base64 != nil:
		raw, err := base64.StdEncoding.DecodeString(*payload.Base64)
		if err != nil {
			return err
		}
		p.Raw = raw
	default:
		return fmt.Errorf("invalid git path encoding")
	}
	return nil
}

func compareGitPath(a, b GitPath) int {
	return bytes.Compare(a.Raw, b.Raw)
}

func compareOptionalPath(a, b *GitPath) int {
	return bytes.Compare(optionalPathBytes(a), optionalPathBytes(b))
}

func optionalPathBytes(path *GitPath) []byte {
	if path == nil {
		return nil
	}
	return path.Raw
}

func compareCandidates(a, b LineCandidate) int {
	if n := compareGitPath(a.Path, b.Path); n != 0 {
		return n
	}
	return compareOptionalPath(a.BasePath, b.BasePath)
}
