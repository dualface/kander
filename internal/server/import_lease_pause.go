package server

import (
	"path/filepath"
	"time"

	"github.com/dualface/kander/v2/internal/fs"
)

// beginImportPause marks that a board import holds the project lock from now
// on. The caller holds mu.
func (p *project) beginImportPause() {
	p.importLockedAt = p.server.now()
	p.importCrashed = false
}

// takeImportPause returns when the board import that holds mu took it, and
// clears the marks of that import. It returns the zero time when no import
// holds mu, when the import failed so that the project refuses every board
// request until a restart, or when a simulated crash ended it: a dead process
// writes no lease, and the restart settles the leases (startupLeases).
func (p *project) takeImportPause() time.Time {
	lockedAt, crashed := p.importLockedAt, p.importCrashed
	p.importLockedAt, p.importCrashed = time.Time{}, false
	if crashed || p.importBroken {
		return time.Time{}
	}
	return lockedAt
}

// pauseImportLeases runs just before a board import releases mu. No request
// can read or renew a lease while the import holds mu, so the time the import
// held it does not count against the leases that were active when it took the
// lock: their expiry moves forward by that time, which outside the lock reads
// the same as if every holder had renewed throughout. A lease that had lapsed
// before the import took the lock stays lapsed. While the publication intent
// remains, the board must keep the digest the intent names for its recovery,
// so nothing is written and the restart settles the leases instead. A failed
// write is logged and never changes the import result, which may already be
// published.
func (p *project) pauseImportLeases(lockedAt time.Time) {
	held := p.server.now().Sub(lockedAt)
	if held <= 0 {
		return
	}
	pending, err := fs.RegularFileExists(p.root, filepath.Join(p.root, "control", "import-pending.json"))
	if err != nil {
		p.log().Error("board import left leases unpaused: publication intent unreadable", "error", err)
		return
	}
	if pending {
		p.log().Warn("board import left leases unpaused until its publication is recovered", "held", held)
		return
	}
	_ = p.extendActiveLeases(func(expires time.Time) (time.Time, bool) {
		return expires.Add(held), expires.After(lockedAt)
	}, func(err error) error {
		p.log().Error("board import could not pause a lease", "error", err)
		return nil
	})
}
