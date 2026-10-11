// Package rules embeds the published Kander markdown rule files, prompt templates, and
// phase snippets.
//
// Three kinds of files live here, all English only:
//
//   - rule files at the top level (`KANDER-*.md`), which an agent reads itself;
//   - `templates/`, Go text/template sources that Kander renders and hands to an agent;
//   - `snippets/`, static rule excerpts that an agent reads when Kander names them.
//
// Every file is addressed by its slash-separated path relative to this directory, the same
// relative path the installer uses below the rules root. The language an agent uses to talk to
// the user is the agent_language config value, which the rules themselves instruct the agent to
// honor, so no per-language copies exist.
package rules

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

//go:embed *.md templates snippets
var content embed.FS

// TemplatesDir and SnippetsDir are the relative directories below the rules root.
const (
	TemplatesDir = "templates"
	SnippetsDir  = "snippets"
)

// Source tells where Installed found a file.
type Source string

const (
	// SourceInstalled means the file was read from the installed rules root.
	SourceInstalled Source = "installed"
	// SourceEmbedded means the installed copy is missing and the embedded copy was used.
	SourceEmbedded Source = "embedded"
)

// Names returns the slash-separated relative paths of every embedded markdown file, sorted.
func Names() []string {
	var names []string
	err := fs.WalkDir(content, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		names = append(names, p)
		return nil
	})
	if err != nil {
		return nil
	}
	sort.Strings(names)
	return names
}

// File returns the bytes of the embedded file at the relative path name.
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

// Hash returns the SHA-256 hex digest of the embedded file at the relative path name.
func Hash(name string) (string, error) {
	data, err := File(name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Installed returns the file at the relative path name below rulesDir, falling back to the
// embedded copy when the installed file does not exist. The returned Source says which copy
// was used, so the caller can tell the user that the installation is incomplete. Any other
// read error, and an invalid name, are returned as errors. Nothing is written.
func Installed(rulesDir, name string) ([]byte, Source, error) {
	if err := validateName(name); err != nil {
		return nil, "", err
	}
	if rulesDir != "" {
		data, err := os.ReadFile(filepath.Join(rulesDir, filepath.FromSlash(name)))
		if err == nil {
			return data, SourceInstalled, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, "", fmt.Errorf("rules: read installed %s: %w", name, err)
		}
	}
	data, err := File(name)
	if err != nil {
		return nil, "", err
	}
	return data, SourceEmbedded, nil
}

// Render executes the template at the relative path name (which must lie under templates/)
// with data, reading the installed copy per Installed. Missing data keys and template errors
// fail the whole render; no partial text is returned. Data values are never parsed as
// template text.
func Render(rulesDir, name string, data any) (string, Source, error) {
	if !strings.HasPrefix(name, TemplatesDir+"/") {
		return "", "", fmt.Errorf("rules: %q is not a template path", name)
	}
	text, source, err := Installed(rulesDir, name)
	if err != nil {
		return "", "", err
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(string(text))
	if err != nil {
		return "", "", fmt.Errorf("rules: parse %s: %w", name, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return "", "", fmt.Errorf("rules: render %s: %w", name, err)
	}
	return out.String(), source, nil
}

// RenderSet executes the template named name from the set of every embedded file below dir
// (a relative directory such as "templates/review"), each read per Installed and registered
// under its base name, so templates may include each other with {{template "x.md" .}}. The
// Source is SourceEmbedded when any file of the set fell back to the embedded copy.
func RenderSet(rulesDir, dir, name string, data any) (string, Source, error) {
	if !strings.HasPrefix(dir, TemplatesDir+"/") && dir != TemplatesDir {
		return "", "", fmt.Errorf("rules: %q is not a template directory", dir)
	}
	set := template.New(dir).Option("missingkey=error")
	source := SourceInstalled
	found := false
	for _, rel := range Names() {
		if !strings.HasPrefix(rel, dir+"/") {
			continue
		}
		text, fileSource, err := Installed(rulesDir, rel)
		if err != nil {
			return "", "", err
		}
		if fileSource == SourceEmbedded {
			source = SourceEmbedded
		}
		if _, err := set.New(path.Base(rel)).Parse(string(text)); err != nil {
			return "", "", fmt.Errorf("rules: parse %s: %w", rel, err)
		}
		found = true
	}
	if !found {
		return "", "", fmt.Errorf("rules: no templates below %q", dir)
	}
	var out bytes.Buffer
	if err := set.ExecuteTemplate(&out, name, data); err != nil {
		return "", "", fmt.Errorf("rules: render %s/%s: %w", dir, name, err)
	}
	return out.String(), source, nil
}

// validateName accepts only clean, slash-separated relative paths to markdown files below
// this directory: no absolute paths, no backslashes, no "." or ".." segments.
func validateName(name string) error {
	if name == "" || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return fmt.Errorf("rules: invalid name %q", name)
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("rules: invalid name %q", name)
		}
	}
	if !strings.HasSuffix(name, ".md") {
		return fmt.Errorf("rules: not a markdown rule file: %q", name)
	}
	return nil
}
