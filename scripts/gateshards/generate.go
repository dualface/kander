package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// PlatformConfig says how the generator builds the shards of one platform.
type PlatformConfig struct {
	Name     string
	OS       string
	Parallel int
	Timeout  string
	Filters  map[string]Filter
	// CommonSeconds is the time the common steps add to the common shard.
	CommonSeconds float64
}

// DefaultConfigs are the platforms of release-gate.yml. CommonSeconds come
// from the step times of run 46132427480: build, vet, Import and concurrent
// locks.
var DefaultConfigs = []PlatformConfig{
	{
		Name: "ubuntu", OS: "ubuntu-latest", Parallel: 4, Timeout: "30m",
		CommonSeconds: 181,
	},
	{
		// macOS runners have 3 vCPUs; two packages at a time leaves room for
		// the timing-sensitive host start tests of cmd/kander.
		Name: "macos", OS: "macos-latest", Parallel: 2, Timeout: "30m",
		Filters:       map[string]Filter{"internal/board": {Skip: "^TestModelCompositeStates$"}},
		CommonSeconds: 261,
	},
	{
		Name: "windows", OS: "windows-latest", Parallel: 4, Timeout: "30m",
		Filters: map[string]Filter{
			"internal/board":  {Skip: "^TestModel"},
			"internal/remote": {Run: "^TestWindows"},
		},
		CommonSeconds: 245,
	},
}

// Timings holds, per package, the package time and its top-level test times.
type Timings struct {
	Packages map[string]*PackageTiming
}

// PackageTiming is the time of one package in seconds.
type PackageTiming struct {
	Seconds float64
	Tests   map[string]float64
}

// ReadTimings reads go test -json files. module is the module path that the
// import paths are made relative to. A package that appears in several files
// (Windows runs internal/server in two steps) adds up.
func ReadTimings(module string, files []string) (*Timings, error) {
	timings := &Timings{Packages: map[string]*PackageTiming{}}
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1<<20), 64<<20)
		for scanner.Scan() {
			var event struct {
				Action  string
				Package string
				Test    string
				Elapsed *float64
			}
			if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Elapsed == nil {
				continue
			}
			if event.Action != "pass" && event.Action != "fail" {
				continue
			}
			rel, ok := relPackage(module, event.Package)
			if !ok {
				continue
			}
			pkg := timings.Packages[rel]
			if pkg == nil {
				pkg = &PackageTiming{Tests: map[string]float64{}}
				timings.Packages[rel] = pkg
			}
			switch {
			case event.Test == "":
				pkg.Seconds += *event.Elapsed
			case !strings.Contains(event.Test, "/"):
				pkg.Tests[event.Test] += *event.Elapsed
			}
		}
		err = scanner.Err()
		f.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
	}
	return timings, nil
}

func relPackage(module, importPath string) (string, bool) {
	if importPath == module {
		return ".", true
	}
	rel, ok := strings.CutPrefix(importPath, module+"/")
	return rel, ok
}

// unit is what the packer places: a whole package, which runs in the shard's
// parallel call, or a part of a split package, which runs in a call of its
// own.
type unit struct {
	seconds float64
	entry   Entry
	serial  bool
}

// shardLoad is the packer's view of one shard.
type shardLoad struct {
	units       []unit
	serial      float64
	wholeSum    float64
	wholeMax    float64
	commonExtra float64
}

// parallelEfficiency discounts -p: packages that run together slow each other
// down.
const parallelEfficiency = 0.75

func (s *shardLoad) estimate(parallel int, extra unit) float64 {
	serial, wholeSum, wholeMax := s.serial, s.wholeSum, s.wholeMax
	if extra.seconds > 0 {
		if extra.serial {
			serial += extra.seconds
		} else {
			wholeSum += extra.seconds
			wholeMax = math.Max(wholeMax, extra.seconds)
		}
	}
	return s.commonExtra + serial + math.Max(wholeMax, wholeSum/(float64(parallel)*parallelEfficiency))
}

func (s *shardLoad) add(u unit) {
	s.units = append(s.units, u)
	if u.serial {
		s.serial += u.seconds
	} else {
		s.wholeSum += u.seconds
		s.wholeMax = math.Max(s.wholeMax, u.seconds)
	}
}

// splitPackage cuts a package that takes longer than target into Tests
// entries of its largest tests and one owner entry that skips them.
func splitPackage(pkg string, timing *PackageTiming, target float64) []unit {
	type test struct {
		name    string
		seconds float64
	}
	var tests []test
	for name, seconds := range timing.Tests {
		tests = append(tests, test{name, seconds})
	}
	slices.SortFunc(tests, func(a, b test) int {
		if a.seconds != b.seconds {
			if a.seconds > b.seconds {
				return -1
			}
			return 1
		}
		return strings.Compare(a.name, b.name)
	})
	rest := timing.Seconds
	var units []unit
	var chunk []string
	chunkSeconds := 0.0
	flush := func() {
		if len(chunk) > 0 {
			slices.Sort(chunk)
			units = append(units, unit{seconds: chunkSeconds, serial: true, entry: Entry{Package: pkg, Tests: chunk}})
			chunk, chunkSeconds = nil, 0
		}
	}
	var skipped []string
	for _, t := range tests {
		if rest <= target {
			break
		}
		if chunkSeconds+t.seconds > target && len(chunk) > 0 {
			flush()
		}
		chunk = append(chunk, t.name)
		skipped = append(skipped, t.name)
		chunkSeconds += t.seconds
		rest -= t.seconds
		if chunkSeconds >= target {
			flush()
		}
	}
	flush()
	if len(units) == 0 {
		return []unit{{seconds: timing.Seconds, entry: Entry{Package: pkg}}}
	}
	slices.Sort(skipped)
	return append(units, unit{seconds: math.Max(rest, 0), serial: true, entry: Entry{Package: pkg, Skip: skipped}})
}

// GeneratePlatform packs the platform's packages into the fewest shards whose
// estimates stay within target seconds; a unit longer than target gets a shard
// of its own.
func GeneratePlatform(config PlatformConfig, timings *Timings, target float64) Platform {
	var units []unit
	pkgs := make([]string, 0, len(timings.Packages))
	for pkg := range timings.Packages {
		pkgs = append(pkgs, pkg)
	}
	slices.Sort(pkgs)
	for _, pkg := range pkgs {
		timing := timings.Packages[pkg]
		if timing.Seconds > target && config.Filters[pkg].Run == "" {
			units = append(units, splitPackage(pkg, timing, target)...)
		} else {
			units = append(units, unit{seconds: timing.Seconds, entry: Entry{Package: pkg}})
		}
	}
	slices.SortStableFunc(units, func(a, b unit) int {
		switch {
		case a.seconds > b.seconds:
			return -1
		case a.seconds < b.seconds:
			return 1
		}
		return 0
	})
	var loads []*shardLoad
	for count := 1; ; count++ {
		loads = pack(units, count, config)
		fits := true
		for _, load := range loads {
			if load.estimate(config.Parallel, unit{}) > target && len(load.units) > 1 {
				fits = false
			}
		}
		if fits || count >= len(units)+1 {
			break
		}
	}
	platform := Platform{Name: config.Name, OS: config.OS, Parallel: config.Parallel, Timeout: config.Timeout, Filters: config.Filters}
	for i, load := range loads {
		shard := Shard{Name: fmt.Sprintf("%d", i+1), EstimateSeconds: int(math.Round(load.estimate(config.Parallel, unit{})))}
		if i == 0 {
			shard.Common, shard.Rest = true, true
		}
		for _, u := range load.units {
			shard.Entries = append(shard.Entries, u.entry)
		}
		slices.SortFunc(shard.Entries, func(a, b Entry) int { return strings.Compare(a.Package, b.Package) })
		platform.Shards = append(platform.Shards, shard)
	}
	return platform
}

// pack places units, largest first, on the shard whose estimate grows least.
// Shard 0 carries the common steps.
func pack(units []unit, count int, config PlatformConfig) []*shardLoad {
	loads := make([]*shardLoad, count)
	for i := range loads {
		loads[i] = &shardLoad{}
	}
	loads[0].commonExtra = config.CommonSeconds
	for _, u := range units {
		best := 0
		for i := range loads {
			if loads[i].estimate(config.Parallel, u) < loads[best].estimate(config.Parallel, u) {
				best = i
			}
		}
		loads[best].add(u)
	}
	return loads
}

// Generate builds a plan from the artifact directory of one gate run.
func Generate(module, artifacts, source string, configs []PlatformConfig, target float64) (*Plan, error) {
	plan := &Plan{Schema: 1, GeneratedFrom: source}
	for _, config := range configs {
		files, err := platformSources(artifacts, config.OS)
		if err != nil {
			return nil, fmt.Errorf("platform %s: %w", config.Name, err)
		}
		timings, err := ReadTimings(module, files)
		if err != nil {
			return nil, fmt.Errorf("platform %s: %w", config.Name, err)
		}
		if len(timings.Packages) == 0 {
			return nil, fmt.Errorf("platform %s: no package timings in %v", config.Name, files)
		}
		plan.Platforms = append(plan.Platforms, GeneratePlatform(config, timings, target))
	}
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("the generated plan is invalid: %w", err)
	}
	return plan, nil
}

// commonStepFiles are the go test -json files of the common steps and of the
// extra Windows packages of a dispatch. They run outside the plan, in the
// common shard, so their timings do not feed it.
var commonStepFiles = []string{"import.json", "concurrent-locks.json", "extra.json"}

// platformSources lists the go test -json files of the native artifacts of
// one runner OS, test-json-native-<os>-*/*.json in the directory gh run
// download writes, without the files of the common steps. Both the gate before
// sharding (one artifact per job) and the sharded gate (one per shard) name
// their artifacts this way.
func platformSources(artifacts, os string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(artifacts, "test-json-native-"+os+"-*", "*.json"))
	if err != nil {
		return nil, err
	}
	files = slices.DeleteFunc(files, func(file string) bool {
		return slices.Contains(commonStepFiles, filepath.Base(file))
	})
	if len(files) == 0 {
		return nil, fmt.Errorf("no test-json-native-%s-* artifact files in %s", os, artifacts)
	}
	slices.Sort(files)
	return files, nil
}
