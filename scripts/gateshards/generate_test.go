package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const testModule = "example.com/m"

// writeArtifact writes one go test -json file of a gate artifact.
func writeArtifact(t *testing.T, root, dir, file string, lines ...string) {
	t.Helper()
	path := filepath.Join(root, dir, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReadTimingsAddsUpPackagesAndTopLevelTests checks that subtests and
// non-JSON lines are ignored and that a package in two files adds up.
func TestReadTimingsAddsUpPackagesAndTopLevelTests(t *testing.T) {
	root := t.TempDir()
	writeArtifact(t, root, "a", "x.json",
		`{"Action":"pass","Package":"example.com/m/p","Test":"TestA","Elapsed":3}`,
		`{"Action":"pass","Package":"example.com/m/p","Test":"TestA/sub","Elapsed":2}`,
		`{"Action":"output","Package":"example.com/m/p","Output":"ok\n"}`,
		`not json`,
		`{"Action":"pass","Package":"example.com/m/p","Elapsed":5}`,
		`{"Action":"pass","Package":"other.org/q","Elapsed":9}`,
	)
	writeArtifact(t, root, "b", "y.json",
		`{"Action":"fail","Package":"example.com/m/p","Test":"TestB","Elapsed":7}`,
		`{"Action":"fail","Package":"example.com/m/p","Elapsed":7}`,
	)
	timings, err := ReadTimings(testModule, []string{filepath.Join(root, "a", "x.json"), filepath.Join(root, "b", "y.json")})
	if err != nil {
		t.Fatal(err)
	}
	if len(timings.Packages) != 1 {
		t.Fatalf("packages %v, want only p", timings.Packages)
	}
	p := timings.Packages["p"]
	if p.Seconds != 12 || p.Tests["TestA"] != 3 || p.Tests["TestB"] != 7 || len(p.Tests) != 2 {
		t.Errorf("p = %+v, want 12 s with TestA 3 and TestB 7", p)
	}
}

// TestSplitPackageOwnerSkipsExactlyTheListedTests checks that a package over
// the target is cut into Tests entries and one owner that skips exactly them.
func TestSplitPackageOwnerSkipsExactlyTheListedTests(t *testing.T) {
	timing := &PackageTiming{Seconds: 100, Tests: map[string]float64{"TestBig": 60, "TestMid": 25, "TestA": 5, "TestB": 5, "TestC": 5}}
	units := splitPackage("p", timing, 30)
	var listed, skipped []string
	owners := 0
	for _, u := range units {
		if !u.serial {
			t.Errorf("unit %+v of a split package runs in the parallel call", u)
		}
		listed = append(listed, u.entry.Tests...)
		if len(u.entry.Skip) > 0 {
			owners++
			skipped = u.entry.Skip
			if u.seconds > 30 {
				t.Errorf("the owner keeps %v s, want at most the 30 s target", u.seconds)
			}
		}
	}
	slices.Sort(listed)
	if owners != 1 || !slices.Equal(listed, skipped) {
		t.Errorf("owners %d, listed %v, skipped %v; want one owner that skips the listed tests", owners, listed, skipped)
	}
	if !slices.Contains(listed, "TestBig") || slices.Contains(listed, "TestC") {
		t.Errorf("listed %v, want the largest tests and not the small ones", listed)
	}
}

// TestSplitPackageKeepsASmallPackageWhole checks the package under the target.
func TestSplitPackageKeepsASmallPackageWhole(t *testing.T) {
	units := splitPackage("p", &PackageTiming{Seconds: 20, Tests: map[string]float64{"TestA": 20}}, 30)
	if len(units) != 1 || units[0].serial || len(units[0].entry.Tests)+len(units[0].entry.Skip) != 0 {
		t.Errorf("units %+v, want the whole package", units)
	}
}

// TestGenerateCoversEveryPackageAndSeparatesTheCommonShard builds a plan
// from two artifacts and checks that it is valid, names every package, puts a
// unit longer than the target alone, and keeps the filtered package whole.
func TestGenerateCoversEveryPackageAndSeparatesTheCommonShard(t *testing.T) {
	root := t.TempDir()
	writeArtifact(t, root, "test-json-native-windows-latest-services", "services.json",
		`{"Action":"pass","Package":"example.com/m/big","Test":"TestHuge","Elapsed":400}`,
		`{"Action":"pass","Package":"example.com/m/big","Test":"TestSmall","Elapsed":20}`,
		`{"Action":"pass","Package":"example.com/m/big","Elapsed":420}`,
		`{"Action":"pass","Package":"example.com/m/a","Elapsed":90}`,
		`{"Action":"pass","Package":"example.com/m/b","Elapsed":90}`,
		`{"Action":"pass","Package":"example.com/m/remote","Test":"TestWindowsX","Elapsed":500}`,
		`{"Action":"pass","Package":"example.com/m/remote","Elapsed":500}`,
	)
	// Files of the common steps are not timings of the plan.
	writeArtifact(t, root, "test-json-native-windows-latest-services", "import.json",
		`{"Action":"pass","Package":"example.com/m/imported","Elapsed":300}`)
	config := PlatformConfig{Name: "windows", OS: "windows-latest", Parallel: 2, Timeout: "5m",
		Filters: map[string]Filter{"remote": {Run: "^TestWindows"}}, CommonSeconds: 50}
	plan, err := Generate(testModule, root, "test", []PlatformConfig{config}, 200)
	if err != nil {
		t.Fatal(err)
	}
	platform := plan.Platforms[0]
	named := platform.NamedPackages()
	for _, pkg := range []string{"big", "a", "b", "remote"} {
		if !named[pkg] {
			t.Errorf("the plan does not name %s", pkg)
		}
	}
	if named["imported"] {
		t.Errorf("the plan names imported, a package of the common steps only")
	}
	for _, shard := range platform.Shards {
		for _, entry := range shard.Entries {
			if slices.Contains(entry.Tests, "TestHuge") && len(shard.Entries) != 1 {
				t.Errorf("shard %s holds TestHuge with %d other entries, want it alone", shard.Name, len(shard.Entries)-1)
			}
			if entry.Package == "remote" && len(entry.Tests)+len(entry.Skip) != 0 {
				t.Errorf("remote has a -run filter but was split: %+v", entry)
			}
		}
		if shard.Common != (shard.Name == "1") || shard.Rest != (shard.Name == "1") {
			t.Errorf("shard %s: common %v rest %v, want both on shard 1 only", shard.Name, shard.Common, shard.Rest)
		}
	}
}

// TestGenerateFailsWithoutArtifacts keeps a wrong directory from producing an
// empty plan.
func TestGenerateFailsWithoutArtifacts(t *testing.T) {
	_, err := Generate(testModule, t.TempDir(), "test", DefaultConfigs, 300)
	if err == nil || !strings.Contains(err.Error(), "no test-json-native-ubuntu-latest-*") {
		t.Errorf("Generate() = %v, want a missing artifact error", err)
	}
}
