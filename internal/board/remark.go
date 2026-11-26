package board

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Remark reports a note of a board operation that is not an error: text is ready to print, and
// code names the kind for a caller that wants it structured. The default writes text to
// stderr; the command layer replaces it so that --json carries the note as a notice.
var Remark = func(_, text string) { _, _ = os.Stderr.WriteString(text) }

type remarkRoute struct {
	sink func(code, text string)
}

var (
	remarkMu     sync.RWMutex
	remarkRoutes = make(map[string][]*remarkRoute)
)

// RouteRemarks sends remarks for a board to sink until the returned function
// is called. The newest remaining registration wins. Calls already in flight
// may finish after release.
func RouteRemarks(root string, sink func(code, text string)) (release func()) {
	root = filepath.Clean(root)
	route := &remarkRoute{sink: sink}
	remarkMu.Lock()
	remarkRoutes[root] = append(remarkRoutes[root], route)
	remarkMu.Unlock()
	return func() {
		remarkMu.Lock()
		defer remarkMu.Unlock()
		routes := remarkRoutes[root]
		if i := slices.Index(routes, route); i >= 0 {
			routes = slices.Delete(routes, i, i+1)
			if len(routes) == 0 {
				delete(remarkRoutes, root)
			} else {
				remarkRoutes[root] = routes
			}
		}
	}
}

func remark(root, code, text string) {
	remarkMu.RLock()
	routes := remarkRoutes[filepath.Clean(root)]
	var route *remarkRoute
	if len(routes) > 0 {
		route = routes[len(routes)-1]
	}
	remarkMu.RUnlock()
	if route != nil {
		route.sink(code, text)
		return
	}
	Remark(code, text)
}
