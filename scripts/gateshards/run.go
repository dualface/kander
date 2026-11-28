package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Call is one go test call of a shard: the arguments after `go test -json`.
type Call struct {
	Name string
	Args []string
}

// Calls turns one shard into go test calls. universe is the list of packages
// the platform tests, as repository-relative directories; the rest shard runs
// those that no entry of the plan names.
//
//   - Whole packages without a filter run in one call with -p Parallel.
//   - A whole package with a filter, an owner entry and a Tests entry each run
//     in a call of their own, because -run and -skip apply to every package
//     of a call.
//   - An owner call passes -run . : it still runs every test it does not skip,
//     and it tells a TestMain that checks something over a whole-package run
//     (internal/server checks that the tests compared every write route) that
//     this run is partial. Plan.Validate keeps every split package whole on
//     another platform, where that check runs.
func Calls(platform *Platform, shard *Shard, universe []string) []Call {
	var plain []string
	var calls []Call
	addFiltered := func(name, pkg string, run, skip []string) {
		filter := platform.Filters[pkg]
		args := []string{"-p", "1", "-count=1", "-timeout", platform.Timeout}
		runPattern := joinPatterns(filter.Run, exactTests(run))
		if runPattern == "" && len(skip) > 0 {
			runPattern = "."
		}
		if runPattern != "" {
			args = append(args, "-run", runPattern)
		}
		if pattern := joinPatterns(filter.Skip, exactTests(skip)); pattern != "" {
			args = append(args, "-skip", pattern)
		}
		calls = append(calls, Call{Name: name, Args: append(args, "./"+pkg)})
	}
	whole := func(pkg string) {
		if _, filtered := platform.Filters[pkg]; filtered {
			addFiltered(slug(pkg), pkg, nil, nil)
			return
		}
		plain = append(plain, pkg)
	}
	for i, entry := range shard.Entries {
		switch {
		case len(entry.Tests) > 0:
			addFiltered(fmt.Sprintf("%s-tests-%d", slug(entry.Package), i+1), entry.Package, entry.Tests, nil)
		case len(entry.Skip) > 0:
			addFiltered(slug(entry.Package)+"-owner", entry.Package, nil, entry.Skip)
		default:
			whole(entry.Package)
		}
	}
	if shard.Rest {
		named := platform.NamedPackages()
		for _, pkg := range universe {
			if !named[pkg] {
				whole(pkg)
			}
		}
	}
	if len(plain) > 0 {
		args := []string{"-p", fmt.Sprint(platform.Parallel), "-count=1", "-timeout", platform.Timeout}
		for _, pkg := range plain {
			args = append(args, "./"+pkg)
		}
		calls = append([]Call{{Name: "packages", Args: args}}, calls...)
	}
	return calls
}

// exactTests is the pattern that matches exactly these top-level tests.
func exactTests(tests []string) string {
	if len(tests) == 0 {
		return ""
	}
	return "^(" + strings.Join(tests, "|") + ")$"
}

func joinPatterns(patterns ...string) string {
	return strings.Join(slices.DeleteFunc(patterns, func(p string) bool { return p == "" }), "|")
}

func slug(pkg string) string {
	return strings.NewReplacer("/", "-", ".", "root").Replace(pkg)
}

// ReadUniverse reads the package list of a platform: one import path per
// line, as go list prints it, made repository-relative.
func ReadUniverse(module, path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkgs []string
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rel, ok := relPackage(module, line)
		if !ok {
			return nil, fmt.Errorf("%s: %q is not a package of module %s", path, line, module)
		}
		pkgs = append(pkgs, rel)
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("%s lists no packages", path)
	}
	return pkgs, nil
}

// RunShard runs every call of a shard through scripts/go-test-json.sh, one
// after the other, and keeps going after a failure so that one failing call
// does not hide the others. It returns an error naming the failed calls.
func RunShard(root, jsonDir, platformName, shardName string, calls []Call, stdout, stderr io.Writer) error {
	if len(calls) == 0 {
		return fmt.Errorf("shard %s of %s has no go test calls", shardName, platformName)
	}
	var failed []string
	for _, call := range calls {
		jsonFile := filepath.Join(jsonDir, call.Name+".json")
		fmt.Fprintf(stdout, "::group::go test %s\n", strings.Join(call.Args, " "))
		cmd := exec.Command("bash", append([]string{filepath.Join(root, "scripts", "go-test-json.sh"), jsonFile}, call.Args...)...)
		cmd.Dir = root
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		err := cmd.Run()
		fmt.Fprintln(stdout, "::endgroup::")
		if err != nil {
			fmt.Fprintf(stdout, "::error::go test %s failed: %v\n", call.Name, err)
			failed = append(failed, call.Name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed go test calls: %s", strings.Join(failed, ", "))
	}
	return nil
}
