package board

import (
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRouteRemarksReleaseOwnRegistration(t *testing.T) {
	root := t.TempDir()
	var calls []string
	sink := func(name string) func(string, string) {
		return func(code, text string) {
			calls = append(calls, name)
			if code != "test-code" || text != "test text\n" {
				t.Errorf("remark = %q, %q", code, text)
			}
		}
	}
	old := Remark
	Remark = sink("fallback")
	t.Cleanup(func() { Remark = old })

	first := RouteRemarks(root+string(os.PathSeparator)+".", sink("first"))
	second := RouteRemarks(root, sink("second"))
	t.Cleanup(first)
	t.Cleanup(second)
	emit := func() { remark(root, "test-code", "test text\n") }
	emit()
	first()
	first()
	emit()
	second()
	emit()

	first = RouteRemarks(root, sink("first"))
	second = RouteRemarks(root, sink("second"))
	t.Cleanup(first)
	t.Cleanup(second)
	second()
	emit()
	first()
	emit()
	want := []string{"second", "second", "fallback", "first", "fallback"}
	if !slices.Equal(calls, want) {
		t.Fatalf("called %v, want %v", calls, want)
	}
}

func TestRouteRemarksConcurrent(t *testing.T) {
	root := t.TempDir()
	var calls atomic.Int64
	sink := func(string, string) { calls.Add(1) }
	release := RouteRemarks(root, sink)
	t.Cleanup(release)
	const workers, rounds = 8, 50
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range rounds {
				remove := RouteRemarks(root, sink)
				remark(root, "code", "text")
				remove()
			}
		})
	}
	wg.Wait()
	if got, want := calls.Load(), int64(workers*rounds); got != want {
		t.Fatalf("delivered %d remarks, want %d", got, want)
	}
}
