package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

// Plan is the shard plan of release-gate.yml, the committed file
// .github/release-gate-shards.json. Each platform lists its shards; each shard
// is one matrix job of the native job.
type Plan struct {
	Schema        int        `json:"schema"`
	GeneratedFrom string     `json:"generated_from"`
	Platforms     []Platform `json:"platforms"`
}

// Platform is one runner platform of the gate.
type Platform struct {
	// Name is the short platform name: ubuntu, macos or windows.
	Name string `json:"name"`
	// OS is the runs-on label of its jobs.
	OS string `json:"os"`
	// Parallel is the go test -p value of the call that runs whole packages.
	Parallel int `json:"parallel"`
	// Timeout is the go test -timeout value of every call.
	Timeout string `json:"timeout"`
	// Filters hold the -run and -skip patterns that always apply to a package
	// on this platform, such as -skip '^TestModel' for internal/board on
	// Windows.
	Filters map[string]Filter `json:"filters,omitempty"`
	Shards  []Shard           `json:"shards"`
}

// Filter is the fixed test selection of one package on one platform.
type Filter struct {
	Run  string `json:"run,omitempty"`
	Skip string `json:"skip,omitempty"`
}

// Shard is one matrix job.
type Shard struct {
	Name string `json:"name"`
	// Common marks the one shard per platform that also runs the build, vet,
	// Import and concurrent lock steps.
	Common bool `json:"common,omitempty"`
	// Rest marks the one shard per platform that runs every package of the
	// platform that no entry of the plan names, so a new package is tested
	// without a new plan.
	Rest bool `json:"rest,omitempty"`
	// EstimateSeconds is the estimate the generator packed the shard by.
	EstimateSeconds int     `json:"estimate_seconds"`
	Entries         []Entry `json:"entries"`
}

// Entry selects tests of one package, by repository-relative directory.
//
//   - Neither Tests nor Skip: the whole package.
//   - Tests: only these top-level tests.
//   - Skip: the package without these tests; the owner of a split package.
//     Its skip list is exactly the tests the package's Tests entries name, so
//     a test that is added later runs in the owner.
type Entry struct {
	Package string   `json:"package"`
	Tests   []string `json:"tests,omitempty"`
	Skip    []string `json:"skip,omitempty"`
}

// LoadPlan reads and validates a plan file.
func LoadPlan(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plan Plan
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&plan); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &plan, nil
}

// Platform returns the named platform.
func (p *Plan) Platform(name string) (*Platform, error) {
	for i := range p.Platforms {
		if p.Platforms[i].Name == name {
			return &p.Platforms[i], nil
		}
	}
	return nil, fmt.Errorf("the plan has no platform %q", name)
}

// Shard returns the named shard of the platform.
func (p *Platform) Shard(name string) (*Shard, error) {
	for i := range p.Shards {
		if p.Shards[i].Name == name {
			return &p.Shards[i], nil
		}
	}
	return nil, fmt.Errorf("platform %s has no shard %q", p.Name, name)
}

// NamedPackages returns every package an entry of the platform names.
func (p *Platform) NamedPackages() map[string]bool {
	named := map[string]bool{}
	for _, shard := range p.Shards {
		for _, entry := range shard.Entries {
			named[entry.Package] = true
		}
	}
	return named
}

// Validate checks the rules that keep the plan from dropping a test:
// one common and one rest shard per platform, and for every package either
// exactly one whole entry, or exactly one owner whose skip list equals the
// union of the package's disjoint Tests entries.
func (p *Plan) Validate() error {
	if p.Schema != 1 {
		return fmt.Errorf("schema %d, want 1", p.Schema)
	}
	if len(p.Platforms) == 0 {
		return fmt.Errorf("no platforms")
	}
	seenPlatform := map[string]bool{}
	for _, platform := range p.Platforms {
		if platform.Name == "" || platform.OS == "" {
			return fmt.Errorf("a platform without name or os")
		}
		if seenPlatform[platform.Name] {
			return fmt.Errorf("platform %s is listed twice", platform.Name)
		}
		seenPlatform[platform.Name] = true
		if err := platform.validate(); err != nil {
			return fmt.Errorf("platform %s: %w", platform.Name, err)
		}
	}
	return nil
}

func (p *Platform) validate() error {
	if p.Parallel < 1 {
		return fmt.Errorf("parallel %d, want at least 1", p.Parallel)
	}
	if p.Timeout == "" {
		return fmt.Errorf("no timeout")
	}
	if len(p.Shards) == 0 {
		return fmt.Errorf("no shards")
	}
	common, rest := 0, 0
	seenShard := map[string]bool{}
	type split struct {
		owners int
		skip   []string
		tests  []string
	}
	whole := map[string]int{}
	splits := map[string]*split{}
	for _, shard := range p.Shards {
		if !validName(shard.Name) {
			return fmt.Errorf("shard name %q: use lowercase letters, digits and hyphens", shard.Name)
		}
		if seenShard[shard.Name] {
			return fmt.Errorf("shard %s is listed twice", shard.Name)
		}
		seenShard[shard.Name] = true
		if shard.Common {
			common++
		}
		if shard.Rest {
			rest++
		}
		if len(shard.Entries) == 0 && !shard.Rest {
			return fmt.Errorf("shard %s has no entries and is not the rest shard", shard.Name)
		}
		for _, entry := range shard.Entries {
			if entry.Package == "" || strings.HasPrefix(entry.Package, "/") || strings.HasPrefix(entry.Package, ".") {
				return fmt.Errorf("shard %s: package %q must be a repository-relative directory", shard.Name, entry.Package)
			}
			for _, name := range append(slices.Clone(entry.Tests), entry.Skip...) {
				if !validTestName(name) {
					return fmt.Errorf("shard %s: %q is not a top-level test name", shard.Name, name)
				}
			}
			switch {
			case len(entry.Tests) > 0 && len(entry.Skip) > 0:
				return fmt.Errorf("shard %s: package %s has both tests and skip", shard.Name, entry.Package)
			case len(entry.Tests) == 0 && len(entry.Skip) == 0:
				whole[entry.Package]++
			default:
				if p.Filters[entry.Package].Run != "" {
					return fmt.Errorf("package %s has a fixed -run filter and cannot be split", entry.Package)
				}
				s := splits[entry.Package]
				if s == nil {
					s = &split{}
					splits[entry.Package] = s
				}
				if len(entry.Skip) > 0 {
					s.owners++
					s.skip = append(s.skip, entry.Skip...)
				}
				s.tests = append(s.tests, entry.Tests...)
			}
		}
	}
	if common != 1 || rest != 1 {
		return fmt.Errorf("%d common and %d rest shards, want exactly one of each", common, rest)
	}
	for pkg, count := range whole {
		if count > 1 {
			return fmt.Errorf("package %s is listed whole %d times", pkg, count)
		}
		if splits[pkg] != nil {
			return fmt.Errorf("package %s is listed whole and split", pkg)
		}
	}
	for pkg, s := range splits {
		if s.owners != 1 {
			return fmt.Errorf("split package %s has %d owner entries (with skip), want 1", pkg, s.owners)
		}
		if len(s.tests) == 0 {
			return fmt.Errorf("split package %s has no tests entries", pkg)
		}
		tests := slices.Clone(s.tests)
		slices.Sort(tests)
		if len(slices.Compact(slices.Clone(tests))) != len(tests) {
			return fmt.Errorf("split package %s names a test in two tests entries", pkg)
		}
		skip := slices.Clone(s.skip)
		slices.Sort(skip)
		if !slices.Equal(tests, skip) {
			return fmt.Errorf("split package %s: the owner skips %v, but the tests entries name %v", pkg, skip, tests)
		}
	}
	return nil
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// validTestName accepts a Go test function name, which is safe to place
// between ^( and )$ in a -run or -skip pattern.
func validTestName(name string) bool {
	if !strings.HasPrefix(name, "Test") {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}
