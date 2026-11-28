package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestCommittedPlanIsValid loads the plan release-gate.yml runs.
func TestCommittedPlanIsValid(t *testing.T) {
	plan, err := LoadPlan(filepath.Join("..", "..", filepath.FromSlash(defaultPlan)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, platform := range plan.Platforms {
		names = append(names, platform.Name)
	}
	if !slices.Equal(names, []string{"ubuntu", "macos", "windows"}) {
		t.Errorf("platforms %v, want ubuntu, macos, windows", names)
	}
}

func validPlan() *Plan {
	return &Plan{Schema: 1, Platforms: []Platform{{
		Name: "ubuntu", OS: "ubuntu-latest", Parallel: 4, Timeout: "30m",
		Shards: []Shard{
			{Name: "1", Common: true, Rest: true, Entries: []Entry{{Package: "a"}, {Package: "b", Skip: []string{"TestX", "TestY"}}}},
			{Name: "2", Entries: []Entry{{Package: "b", Tests: []string{"TestY"}}}},
			{Name: "3", Entries: []Entry{{Package: "b", Tests: []string{"TestX"}}}},
		},
	}}}
}

// validPlanWithWhole is validPlan plus a platform that runs b whole.
func validPlanWithWhole() *Plan {
	plan := validPlan()
	plan.Platforms = append(plan.Platforms, Platform{Name: "macos", OS: "macos-latest", Parallel: 1, Timeout: "30m",
		Shards: []Shard{{Name: "1", Common: true, Rest: true, Entries: []Entry{{Package: "b"}}}}})
	return plan
}

// TestPlanValidateRefusesPlansThatDropTests checks the rules that keep every
// test in the gate.
func TestPlanValidateRefusesPlansThatDropTests(t *testing.T) {
	if err := validPlanWithWhole().Validate(); err != nil {
		t.Fatalf("the valid plan: %v", err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Platform)
		want   string
	}{
		{"owner skips a test no entry runs", func(p *Platform) { p.Shards[0].Entries[1].Skip = []string{"TestX", "TestY", "TestZ"} }, "the owner skips"},
		{"a tests entry outside the owner's skip", func(p *Platform) { p.Shards[2].Entries[0].Tests = []string{"TestZ"} }, "the owner skips"},
		{"no owner", func(p *Platform) { p.Shards[0].Entries = p.Shards[0].Entries[:1] }, "0 owner entries"},
		{"two owners", func(p *Platform) {
			p.Shards[1].Entries = append(p.Shards[1].Entries, Entry{Package: "b", Skip: []string{"TestQ"}})
		}, "2 owner entries"},
		{"a test in two entries", func(p *Platform) { p.Shards[1].Entries[0].Tests = []string{"TestX", "TestY"} }, "in two tests entries"},
		{"whole and split", func(p *Platform) { p.Shards[1].Entries = append(p.Shards[1].Entries, Entry{Package: "b"}) }, "whole and split"},
		{"whole twice", func(p *Platform) { p.Shards[1].Entries = append(p.Shards[1].Entries, Entry{Package: "a"}) }, "listed whole 2 times"},
		{"tests and skip together", func(p *Platform) { p.Shards[1].Entries[0].Skip = []string{"TestX"} }, "both tests and skip"},
		{"no rest shard", func(p *Platform) { p.Shards[0].Rest = false }, "0 rest shards"},
		{"two common shards", func(p *Platform) { p.Shards[1].Common = true }, "2 common"},
		{"split package with a run filter", func(p *Platform) { p.Filters = map[string]Filter{"b": {Run: "^TestW"}} }, "cannot be split"},
		{"a pattern instead of a test name", func(p *Platform) { p.Shards[1].Entries[0].Tests = []string{"TestY.*"} }, "not a top-level test name"},
		{"an absolute package", func(p *Platform) { p.Shards[0].Entries[0].Package = "./a" }, "repository-relative"},
		{"a bad shard name", func(p *Platform) { p.Shards[1].Name = "Shard 2" }, "shard name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := validPlanWithWhole()
			tc.change(&plan.Platforms[0])
			err := plan.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() = %v, want an error with %q", err, tc.want)
			}
		})
	}
}

// TestPlanValidateKeepsASplitPackageWholeOnAnotherPlatform keeps the
// whole-package checks of a TestMain running somewhere.
func TestPlanValidateKeepsASplitPackageWholeOnAnotherPlatform(t *testing.T) {
	plan := validPlan()
	err := plan.Validate()
	if err == nil || !strings.Contains(err.Error(), "package b is split on ubuntu and whole on no platform") {
		t.Fatalf("Validate() = %v, want the split package refused", err)
	}
	if err := validPlanWithWhole().Validate(); err != nil {
		t.Errorf("Validate() with b whole on macos = %v, want nil", err)
	}
}

// TestLoadPlanRefusesUnknownFields keeps a misspelled field from being ignored.
func TestLoadPlanRefusesUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	data := `{"schema":1,"platforms":[{"name":"u","os":"ubuntu-latest","parallel":1,"timeout":"1m","shards":[{"name":"1","common":true,"rest":true,"entries":[],"skips":["TestX"]}]}]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPlan(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("LoadPlan() = %v, want an unknown field error", err)
	}
}
