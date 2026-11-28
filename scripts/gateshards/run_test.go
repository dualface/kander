package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func callsByName(calls []Call) map[string][]string {
	byName := map[string][]string{}
	for _, call := range calls {
		byName[call.Name] = call.Args
	}
	return byName
}

// TestCallsSelectEveryTestOnce checks the go test calls of the shards of one
// platform: whole packages share the -p call, a filtered package and the parts
// of a split package get calls of their own, and the rest shard runs the
// packages no entry names.
func TestCallsSelectEveryTestOnce(t *testing.T) {
	platform := &Platform{
		Name: "windows", Parallel: 4, Timeout: "30m",
		Filters: map[string]Filter{"board": {Skip: "^TestModel"}, "remote": {Run: "^TestWindows"}},
		Shards: []Shard{
			{Name: "1", Common: true, Rest: true, Entries: []Entry{{Package: "a"}, {Package: "board", Skip: []string{"TestSlow"}}, {Package: "heavy", Alone: true}}},
			{Name: "2", Entries: []Entry{{Package: "board", Tests: []string{"TestSlow"}}}},
		},
	}
	universe := []string{"a", "board", "new", "remote"}

	calls := Calls(platform, &platform.Shards[0], universe)
	if last := calls[len(calls)-1].Name; last != "packages" {
		t.Errorf("last call %s, want the parallel packages call last", last)
	}
	first := callsByName(calls)
	if want := []string{"-p", "4", "-count=1", "-timeout", "30m", "./a", "./new"}; !slices.Equal(first["packages"], want) {
		t.Errorf("packages call %v, want %v", first["packages"], want)
	}
	if want := []string{"-p", "1", "-count=1", "-timeout", "30m", "-run", ".", "-skip", "^TestModel|^(TestSlow)$", "./board"}; !slices.Equal(first["board-owner"], want) {
		t.Errorf("owner call %v, want %v", first["board-owner"], want)
	}
	if want := []string{"-p", "1", "-count=1", "-timeout", "30m", "-run", "^TestWindows", "./remote"}; !slices.Equal(first["remote"], want) {
		t.Errorf("remote call %v, want %v", first["remote"], want)
	}
	if want := []string{"-p", "1", "-count=1", "-timeout", "30m", "./heavy"}; !slices.Equal(first["heavy"], want) {
		t.Errorf("alone call %v, want %v", first["heavy"], want)
	}
	if len(first) != 4 {
		t.Errorf("shard 1 calls %v, want packages, board-owner, heavy and remote", first)
	}

	second := Calls(platform, &platform.Shards[1], universe)
	want := []string{"-p", "1", "-count=1", "-timeout", "30m", "-run", "^(TestSlow)$", "-skip", "^TestModel", "./board"}
	if len(second) != 1 || !slices.Equal(second[0].Args, want) {
		t.Errorf("shard 2 calls %v, want one call %v", second, want)
	}
}

// TestReadUniverseRefusesForeignAndEmptyLists keeps a broken package list from
// silently testing nothing.
func TestReadUniverseRefusesForeignAndEmptyLists(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.txt")
	os.WriteFile(good, []byte("example.com/m\nexample.com/m/internal/a\n\n"), 0o644)
	if pkgs, err := ReadUniverse(testModule, good); err != nil || !slices.Equal(pkgs, []string{".", "internal/a"}) {
		t.Errorf("ReadUniverse(good) = %v, %v", pkgs, err)
	}
	for name, data := range map[string]string{"empty": "\n", "foreign": "other.org/x\n"} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, []byte(data), 0o644)
		if _, err := ReadUniverse(testModule, path); err == nil {
			t.Errorf("ReadUniverse(%s) succeeded, want an error", name)
		}
	}
}

// TestRunShardRunsEveryCallAfterAFailure checks that a failing call fails the
// shard without hiding the calls after it.
func TestRunShardRunsEveryCallAfterAFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake go-test-json.sh is a POSIX shell script")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not on PATH")
	}
	root := t.TempDir()
	log := filepath.Join(root, "calls.log")
	fake := "#!/usr/bin/env bash\necho \"$@\" >> '" + log + "'\ncase \"$*\" in *fail*) exit 1 ;; esac\n"
	os.MkdirAll(filepath.Join(root, "scripts"), 0o755)
	if err := os.WriteFile(filepath.Join(root, "scripts", "go-test-json.sh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	calls := []Call{{Name: "one", Args: []string{"./a"}}, {Name: "two", Args: []string{"./fail"}}, {Name: "three", Args: []string{"./c"}}}
	var stdout bytes.Buffer
	err := RunShard(root, filepath.Join(root, "json"), "ubuntu", "1", calls, &stdout, &stdout)
	if err == nil || !strings.Contains(err.Error(), "two") || strings.Contains(err.Error(), "one") {
		t.Errorf("RunShard() = %v, want an error naming call two only", err)
	}
	data, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 || !strings.HasSuffix(lines[2], "three.json ./c") {
		t.Errorf("calls %q, want all three, the last with its JSON file", lines)
	}
	if !strings.Contains(stdout.String(), "::error::go test two failed") {
		t.Errorf("output lacks the error annotation:\n%s", stdout.String())
	}
}
