package fs

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func absolutePath(path string) (string, error) {
	if path == "" {
		return "", failClosed("abs", path, "empty path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", wrap("abs", path, err)
	}
	return abs, nil
}

func pathAnchor(abs string) (string, error) {
	vol := filepath.VolumeName(abs)
	if runtime.GOOS == "windows" {
		if vol == "" {
			return "", failClosed("anchor", abs, "protected path has no absolute anchor")
		}
		if strings.HasPrefix(abs, `\\`) {
			return vol + `\`, nil
		}
		return vol + `\`, nil
	}
	if !strings.HasPrefix(abs, string(os.PathSeparator)) {
		return "", failClosed("anchor", abs, "protected path has no absolute anchor")
	}
	return string(os.PathSeparator), nil
}

func relativeParts(root, path string) (rootAbs, candidate string, parts []string, err error) {
	rootAbs, err = absolutePath(root)
	if err != nil {
		return "", "", nil, err
	}
	if filepath.IsAbs(path) {
		candidate, err = absolutePath(path)
	} else {
		candidate, err = absolutePath(filepath.Join(rootAbs, path))
	}
	if err != nil {
		return "", "", nil, err
	}
	if !lexicalInside(rootAbs, candidate) {
		return "", "", nil, failClosed("rel", candidate, "path escapes protected root")
	}
	rel, err := filepath.Rel(rootAbs, candidate)
	if err != nil {
		return "", "", nil, failClosed("rel", candidate, "path escapes protected root")
	}
	if rel == "." || rel == "" {
		return rootAbs, candidate, nil, nil
	}
	cleaned := filepath.Clean(rel)
	if strings.HasPrefix(cleaned, "..") {
		return "", "", nil, failClosed("rel", candidate, "path escapes protected root")
	}
	for _, part := range splitParts(cleaned) {
		if part == "" || part == "." || part == ".." {
			return "", "", nil, failClosed("rel", candidate, "invalid protected path")
		}
		parts = append(parts, part)
	}
	return rootAbs, candidate, parts, nil
}

func splitParts(rel string) []string {
	rel = strings.Trim(rel, string(os.PathSeparator))
	if rel == "" {
		return nil
	}
	return strings.Split(rel, string(os.PathSeparator))
}

func lexicalInside(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	sep := string(os.PathSeparator)
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
		candidate = strings.ToLower(candidate)
	}
	if candidate == root {
		return true
	}
	if !strings.HasSuffix(root, sep) {
		root += sep
	}
	return strings.HasPrefix(candidate+sep, root)
}

func validateComponent(name, path string) error {
	if name == "" || name == "." || name == ".." {
		return failClosed("component", path, "invalid protected path component")
	}
	if strings.ContainsAny(name, `/\`+string('\x00')) {
		return failClosed("component", path, "invalid protected path component")
	}
	if runtime.GOOS == "windows" && strings.Contains(name, ":") {
		return failClosed("component", path, "invalid protected path component")
	}
	return nil
}
