package main

import (
	"slices"
	"testing"
)

// TestCompareReportsMissingAndAddedTestsPerSuite checks that the comparison
// groups native jobs by OS and race shards together, and that a test that
// moved between shards of one platform is neither missing nor added.
func TestCompareReportsMissingAndAddedTestsPerSuite(t *testing.T) {
	old := t.TempDir()
	writeArtifact(t, old, "test-json-native-macos-latest-platform", "services.json",
		`{"Action":"pass","Package":"p","Test":"TestA"}`,
		`{"Action":"skip","Package":"p","Test":"TestB"}`,
		`{"Action":"pass","Package":"p","Test":"TestGone"}`)
	writeArtifact(t, old, "test-json-race-server", "race.json", `{"Action":"pass","Package":"p","Test":"TestA"}`)
	newer := t.TempDir()
	writeArtifact(t, newer, "test-json-native-macos-latest-1", "packages.json", `{"Action":"pass","Package":"p","Test":"TestA"}`)
	writeArtifact(t, newer, "test-json-native-macos-latest-2", "p-tests-1.json",
		`{"Action":"pass","Package":"p","Test":"TestB"}`,
		`{"Action":"pass","Package":"p","Test":"TestNew/sub"}`,
		`{"Action":"output","Package":"p","Test":"TestOutputOnly"}`)
	writeArtifact(t, newer, "test-json-dsh-ignored", "x.json", `{"Action":"pass","Package":"p","Test":"TestIgnored"}`)

	oldSets, err := TestSets(old)
	if err != nil {
		t.Fatal(err)
	}
	newSets, err := TestSets(newer)
	if err != nil {
		t.Fatal(err)
	}
	diffs := Compare(oldSets, newSets)
	byGroup := map[string]Difference{}
	for _, diff := range diffs {
		byGroup[diff.Group] = diff
	}
	mac := byGroup["macos-latest native"]
	if !slices.Equal(mac.Missing, []string{"p TestGone"}) || !slices.Equal(mac.Added, []string{"p TestNew/sub"}) {
		t.Errorf("macOS difference %+v, want TestGone missing and TestNew/sub added", mac)
	}
	race := byGroup["ubuntu-latest race"]
	if !slices.Equal(race.Missing, []string{"p TestA"}) || len(race.Added) != 0 {
		t.Errorf("race difference %+v, want TestA missing", race)
	}
	if len(diffs) != 2 {
		t.Errorf("groups %v, want macOS native and race only", diffs)
	}
}
