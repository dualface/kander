package server

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"sync"
	"testing"
)

func indexWriteActions() map[string][]string {
	const project = "POST /api/v1/projects/{project}"
	return map[string][]string{
		// resolution-facts is exercised too, although it does not write (review_actions.go).
		project + "/reviews/actions/{action}":                 {"resolution-facts", "resolve-batch", "extend-plan", "advance", "assign", "disposition", "interpret", "abandon", "map-legacy"},
		project + "/tasks/{task}/dispatches/actions/{action}": {"prepare", "attempt", "reauthorize", "end", "settle", "authorize-wrap-up"},
		project + "/reviews/runtime/{run}/{action}":           {"publish", "update", "artifact", "finalize"},
	}
}

var indexCoverage = struct {
	mu      sync.Mutex
	seen    map[string]bool
	skipped map[string]string
}{seen: map[string]bool{}, skipped: map[string]string{}}

func recordIndexCoverage(pattern, action string) {
	indexCoverage.mu.Lock()
	defer indexCoverage.mu.Unlock()
	indexCoverage.seen[pattern] = true
	if action != "" {
		indexCoverage.seen[pattern+"#"+action] = true
	}
}

// skipIndexDifferentialFixture is only for an environmental skip of a
// fixture (or part of one) that would use IndexDifferentialHandler.
func skipIndexDifferentialFixture(t *testing.T, reason string) {
	t.Helper()
	indexCoverage.mu.Lock()
	indexCoverage.skipped[t.Name()] = reason
	indexCoverage.mu.Unlock()
	t.Skip(reason)
}

func TestMain(m *testing.M) {
	code := m.Run()
	// Use -run for partial runs. -skip does not turn off coverage checking.
	if code == 0 && flag.Lookup("test.run").Value.String() == "" && flag.Lookup("test.list").Value.String() == "" {
		indexCoverage.mu.Lock()
		for _, name := range slices.Sorted(maps.Keys(indexCoverage.skipped)) {
			fmt.Fprintf(os.Stderr, "index differential coverage incomplete: %s: %s\n", name, indexCoverage.skipped[name])
		}
		var missing []string
		for pattern, class := range indexWriteRoutes() {
			if class != "read" && !indexCoverage.seen[pattern] {
				missing = append(missing, pattern)
			}
		}
		for pattern, actions := range indexWriteActions() {
			for _, action := range actions {
				if key := pattern + "#" + action; !indexCoverage.seen[key] {
					missing = append(missing, key)
				}
			}
		}
		slices.Sort(missing)
		for _, key := range missing {
			fmt.Fprintf(os.Stderr, "index differential comparison missing for %s\n", key)
		}
		if len(missing) > 0 && len(indexCoverage.skipped) == 0 {
			code = 1
		}
		indexCoverage.mu.Unlock()
	}
	os.Exit(code)
}
