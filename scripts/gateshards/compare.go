package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// suiteOf maps an artifact directory name to its comparison group, platform
// plus suite: test-json-native-<os>-<job> is "<os> native" and
// test-json-race-<shard> is "ubuntu-latest race".
func suiteOf(dir string) (string, bool) {
	name := filepath.Base(dir)
	if rest, ok := strings.CutPrefix(name, "test-json-native-"); ok {
		for _, os := range []string{"ubuntu-latest", "macos-latest", "windows-latest"} {
			if strings.HasPrefix(rest, os+"-") {
				return os + " native", true
			}
		}
		return "", false
	}
	if strings.HasPrefix(name, "test-json-race-") {
		return "ubuntu-latest race", true
	}
	return "", false
}

// TestSets reads every go test -json file below an artifact directory and
// returns, per group, the set of "package test" names that passed, failed or
// were skipped, subtests included.
func TestSets(artifacts string) (map[string]map[string]bool, error) {
	dirs, err := filepath.Glob(filepath.Join(artifacts, "test-json-*"))
	if err != nil {
		return nil, err
	}
	sets := map[string]map[string]bool{}
	for _, dir := range dirs {
		group, ok := suiteOf(dir)
		if !ok {
			continue
		}
		files, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			return nil, err
		}
		if sets[group] == nil {
			sets[group] = map[string]bool{}
		}
		for _, file := range files {
			if err := addTests(sets[group], file); err != nil {
				return nil, err
			}
		}
	}
	if len(sets) == 0 {
		return nil, fmt.Errorf("no test-json-* artifacts in %s", artifacts)
	}
	return sets, nil
}

func addTests(set map[string]bool, file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		var event struct{ Action, Package, Test string }
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Test == "" {
			continue
		}
		switch event.Action {
		case "pass", "fail", "skip":
			set[event.Package+" "+event.Test] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	return nil
}

// Difference is the comparison of one group.
type Difference struct {
	Group   string
	Missing []string // in the old run only
	Added   []string // in the new run only
}

// Compare returns the differences of every group present in either run.
func Compare(old, new map[string]map[string]bool) []Difference {
	groups := map[string]bool{}
	for group := range old {
		groups[group] = true
	}
	for group := range new {
		groups[group] = true
	}
	var names []string
	for group := range groups {
		names = append(names, group)
	}
	slices.Sort(names)
	var diffs []Difference
	for _, group := range names {
		diff := Difference{Group: group}
		for test := range old[group] {
			if !new[group][test] {
				diff.Missing = append(diff.Missing, test)
			}
		}
		for test := range new[group] {
			if !old[group][test] {
				diff.Added = append(diff.Added, test)
			}
		}
		slices.Sort(diff.Missing)
		slices.Sort(diff.Added)
		diffs = append(diffs, diff)
	}
	return diffs
}
