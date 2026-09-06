// Package rules embeds the published Kander markdown rule files.
//
// Chinese originals live in cn/. English translations live in en/ as matching *.md files.
// Names() treats cn/ as the only source of truth, so non-markdown placeholders in en/ are neither
// enumerated nor installed. Requesting en falls back to the Chinese original when that file is
// missing; requesting cn never falls back.
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

//go:embed cn en
var content embed.FS

const (
	LangCN = "cn"
	LangEN = "en"
)

// Names returns the markdown rule files under cn/, sorted.
func Names() []string {
	entries, err := fs.ReadDir(content, LangCN)
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

// File returns the bytes for name in lang. When lang is en and en/name is missing, it returns the
// Chinese original and reports actual language cn. A cn request never falls back.
func File(lang, name string) (data []byte, actual string, err error) {
	if err := validateName(name); err != nil {
		return nil, "", err
	}
	switch lang {
	case LangEN:
		data, err = content.ReadFile(path.Join(LangEN, name))
		if err == nil {
			return data, LangEN, nil
		}
		data, err = content.ReadFile(path.Join(LangCN, name))
		if err != nil {
			return nil, "", fmt.Errorf("rules: missing %s: %w", name, err)
		}
		return data, LangCN, nil
	default:
		data, err = content.ReadFile(path.Join(LangCN, name))
		if err != nil {
			return nil, "", fmt.Errorf("rules: missing %s: %w", name, err)
		}
		return data, LangCN, nil
	}
}

// Hash returns the SHA-256 hex digest of the bytes File would install for lang and name,
// using the actual language after fallback.
func Hash(lang, name string) (digest, actual string, err error) {
	data, actual, err := File(lang, name)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), actual, nil
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
