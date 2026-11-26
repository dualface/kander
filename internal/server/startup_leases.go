package server

import (
	"path/filepath"
	"strings"

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
	tasks, err := p.serviceNames("claims", ".json", fs.KindFile)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		r, found, err := p.readClaim(task)
		if err != nil {
			return err
		}
		k, err := p.projectClaim(r, found, false)
		if err != nil {
			return err
		}
		if !k.step(serverclaim.Startup{}) || !r.ExpiresAt.Before(deadline) {
			continue
		}
		r.ExpiresAt = deadline
		if err := p.writeClaim(r); err != nil {
			return err
		}
	}
	groups, err := p.serviceNames("group-claims", ".json", fs.KindFile)
	if err != nil {
		return err
	}
	for _, group := range groups {
		r, found, err := p.readGroupClaim(group)
		if err != nil {
			return err
		}
		if !p.projectGroup(r, found, nil).step(servergrouplease.Startup{}) || !r.ExpiresAt.Before(deadline) {
			continue
		}
		r.ExpiresAt = deadline
		if err := writeJSON(p.root, servicePath(groupClaimName(group)), r); err != nil {
			return err
		}
	}
	targets, err := p.serviceNames("integrations", "", fs.KindDirectory)
	if err != nil {
		return err
	}
	for _, id := range targets {
		record, found, err := p.readIntegration(id)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		k, err := p.projectIntegration(record.Value, true, nil)
		if err != nil {
			return err
		}
		if !k.step(serverintegration.Startup{}) || !record.Value.ExpiresAt.Before(deadline) {
			continue
		}
		record.Value.ExpiresAt = deadline
		if err := writeJSON(p.root, servicePath(integrationName(id)), record); err != nil {
			return err
		}
	}
	return nil
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
