package server

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/statemachine/serverclaim"
	"github.com/dualface/kander/v2/internal/statemachine/servergrouplease"
	"github.com/dualface/kander/v2/internal/statemachine/serverintegration"
)

// startupLeases gives every active lease of the project (task claims, group
// leases, and integration target leases) at least one TTL from now, through
// each kernel's startup event. The server's downtime is not the holders'
// fault, so it never opens a takeover. A lease that had already lapsed before
// the server stopped is extended as well: a crashed server records no stop
// time, and the cost is that a takeover waits one more TTL. RenewedAt stays,
// since no holder renewed. Each record is written on its own, as
// reconcileClaims writes claims; a failed write stops the project from
// opening, and opening it again repeats the same writes.
func (p *project) startupLeases() error {
	deadline := p.server.now().Add(p.server.ttl)
	return p.extendActiveLeases(func(expires time.Time) (time.Time, bool) {
		return deadline, expires.Before(deadline)
	}, func(err error) error { return err })
}

// leaseExtension returns the new expiry of an active lease, and false to leave
// the lease as it is.
type leaseExtension func(expires time.Time) (time.Time, bool)

// extendActiveLeases moves the expiry of every active task claim, group lease,
// and integration target lease as extend decides. A record is active when its
// kernel accepts the startup event: the formal model names that event for a
// server start, and a board import that held the project lock uses it with the
// same meaning, since in both cases the server was unavailable to the holders
// through no fault of theirs. Only ExpiresAt changes; RenewedAt stays, since
// no holder renewed. fail receives every error; it returns nil to go on with
// the next record or the error to stop.
func (p *project) extendActiveLeases(extend leaseExtension, fail func(error) error) error {
	kinds := []struct {
		dir, suffix string
		kind        fs.Kind
		extend      func(string, leaseExtension) error
	}{
		{"claims", ".json", fs.KindFile, p.extendClaim},
		{"group-claims", ".json", fs.KindFile, p.extendGroupLease},
		{"integrations", "", fs.KindDirectory, p.extendIntegrationLease},
	}
	for _, k := range kinds {
		names, err := p.serviceNames(k.dir, k.suffix, k.kind)
		if err != nil {
			if err := fail(err); err != nil {
				return err
			}
			continue
		}
		for _, name := range names {
			if err := k.extend(name, extend); err != nil {
				if err := fail(err); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (p *project) extendClaim(task string, extend leaseExtension) error {
	r, found, err := p.readClaim(task)
	if err != nil {
		return err
	}
	k, err := p.projectClaim(r, found, false)
	if err != nil {
		return err
	}
	if !k.step(serverclaim.Startup{}) {
		return nil
	}
	expires, ok := extend(r.ExpiresAt)
	if !ok {
		return nil
	}
	r.ExpiresAt = expires
	return p.writeClaim(r)
}

func (p *project) extendGroupLease(group string, extend leaseExtension) error {
	r, found, err := p.readGroupClaim(group)
	if err != nil {
		return err
	}
	if !p.projectGroup(r, found, nil).step(servergrouplease.Startup{}) {
		return nil
	}
	expires, ok := extend(r.ExpiresAt)
	if !ok {
		return nil
	}
	r.ExpiresAt = expires
	return writeJSON(p.root, servicePath(groupClaimName(group)), r)
}

func (p *project) extendIntegrationLease(id string, extend leaseExtension) error {
	record, found, err := p.readIntegration(id)
	if err != nil || !found {
		return err
	}
	k, err := p.projectIntegration(record.Value, true, nil)
	if err != nil {
		return err
	}
	if !k.step(serverintegration.Startup{}) {
		return nil
	}
	expires, ok := extend(record.Value.ExpiresAt)
	if !ok {
		return nil
	}
	record.Value.ExpiresAt = expires
	return writeJSON(p.root, servicePath(integrationName(id)), record)
}

// serviceNames lists the entries of one kind below a service directory, with
// suffix trimmed from their names. A missing directory has no entries.
func (p *project) serviceNames(dir, suffix string, kind fs.Kind) ([]string, error) {
	path := filepath.Join(p.root, servicePath(dir))
	exists, err := fs.DirectoryExists(p.root, path)
	if err != nil || !exists {
		return nil, err
	}
	entries, err := fs.ListDirectory(p.root, path)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.Kind == kind && strings.HasSuffix(entry.Name, suffix) {
			names = append(names, strings.TrimSuffix(entry.Name, suffix))
		}
	}
	return names, nil
}
