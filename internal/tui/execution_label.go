package tui

import (
	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/remote"
)

// executionLabel is the owner part of the card and detail metadata lines.
// While a server claim holds or was lost, it adds the host of the claiming
// client, so a distributed board shows where each card runs. A client ID
// without a host name is shown whole.
func (a *App) executionLabel(task Task) string {
	owner := orUnassigned(task.Owner, a.Context.Unassigned)
	if task.ClaimClientID == "" {
		return owner
	}
	switch task.ClaimStatus {
	case api.ClaimActive, api.ClaimLapsed, api.ClaimLost:
	default:
		return owner
	}
	host := remote.ClientHost(task.ClaimClientID)
	if host == "" {
		host = task.ClaimClientID
	}
	return owner + "@" + host
}
