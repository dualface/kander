package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type pathCheck struct {
	Current string
	Found   string
	Matches bool
	Err     error
}

// inspectPath compares file identities, following links for this read-only check.
// It never executes a candidate or treats equal binary contents as identity.
func inspectPath(current string) pathCheck {
	check := pathCheck{Current: current}
	if current == "" {
		var err error
		current, err = lookupExecutable()
		check.Current, check.Err = current, err
		if err != nil {
			return check
		}
	}
	currentInfo, err := os.Stat(current)
	if err != nil {
		check.Err = err
		return check
	}
	check.Found, check.Err = firstPathBinary(os.Getenv("PATH"), pathNames())
	if check.Err != nil || check.Found == "" {
		return check
	}
	foundInfo, err := os.Stat(check.Found)
	check.Err = err
	check.Matches = err == nil && os.SameFile(currentInfo, foundInfo)
	return check
}

func pathNames() []string {
	if runtime.GOOS != "windows" {
		return []string{"kander"}
	}
	ext := os.Getenv("PATHEXT")
	if ext == "" {
		ext = ".COM;.EXE;.BAT;.CMD"
	}
	var names []string
	for _, suffix := range strings.Split(ext, ";") {
		if strings.HasPrefix(suffix, ".") && !strings.ContainsAny(suffix, `/\`) {
			names = append(names, "kander"+strings.ToLower(suffix))
		}
	}
	return names
}

// firstPathBinary searches PATH only, excluding Windows' implicit current-directory
// lookup. Unknown inspection failures stop the search instead of claiming a match.
func firstPathBinary(path string, names []string) (string, error) {
	if path == "" {
		return "", nil
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			if runtime.GOOS == "windows" {
				continue
			}
			dir = "."
		}
		for _, name := range names {
			candidate, err := filepath.Abs(filepath.Join(dir, name))
			if err != nil {
				return "", err
			}
			info, err := os.Stat(candidate)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return "", err
			}
			if !info.Mode().IsRegular() {
				continue
			}
			if runtime.GOOS != "windows" {
				if info.Mode()&0o111 == 0 {
					continue
				}
				if _, err := exec.LookPath(candidate); err != nil {
					return "", err
				}
			}
			return candidate, nil
		}
	}
	return "", nil
}
