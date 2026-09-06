// Package rules embeds the published Kander markdown rule files.
//
// The rules ship in English only. The language an agent uses to talk to the user is the
// agent_language config value, which the rules themselves instruct the agent to honor, so no
// per-language rule copies exist.
package rules

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed *.md
var content embed.FS

// Names returns the embedded markdown rule files, sorted.
func Names() []string {
	entries, err := fs.ReadDir(content, ".")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// File returns the bytes of the embedded rule file name.
func File(name string) ([]byte, error) {
	if err := validateName(name); err != nil {
		return nil, err
	}
	data, err := content.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("rules: missing %s: %w", name, err)
	}
	return data, nil
}

// Hash returns the SHA-256 hex digest of the embedded rule file name.
func Hash(name string) (string, error) {
	data, err := File(name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func validateName(name string) error {
	if name == "" || name != path.Base(name) || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return fmt.Errorf("rules: invalid name %q", name)
	}
	if !strings.HasSuffix(name, ".md") {
		return fmt.Errorf("rules: not a markdown rule file: %q", name)
	}
	return nil
}
