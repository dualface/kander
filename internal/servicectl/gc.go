package servicectl

import (
	"context"
	"errors"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
)

// ErrNoServer reports that no server answers at the target, so an online
// call cannot be made.
var ErrNoServer = errors.New("no server answers")

// ServerFor returns the info of the loopback server at target when it serves
// dataDir. It returns ErrNoServer when nothing answers at target, and an error
// when the server serves another data directory.
func ServerFor(ctx context.Context, target, dataDir string) (api.ServerInfo, error) {
	if !Local(target) {
		return api.ServerInfo{}, errors.New(config.Text("gc.local_only", target))
	}
	info, err := Info(ctx, target)
	if client.IsUnreachable(err) {
		return api.ServerInfo{}, ErrNoServer
	}
	if err != nil {
		return api.ServerInfo{}, err
	}
	if !sameDirectory(info.DataDir, dataDir) {
		return api.ServerInfo{}, errors.New(config.Text("gc.data_mismatch", target, info.DataDir, dataDir))
	}
	return info, nil
}

// GC sends one gc request with the administration token to the local server
// that ServerFor found serving dataDir.
func GC(ctx context.Context, info api.ServerInfo, req api.ServerGCRequest) (api.ServerGCReport, error) {
	token, err := adminToken(info.DataDir)
	if err != nil {
		return api.ServerGCReport{}, err
	}
	p, err := peer(info.URL)
	if err != nil {
		return api.ServerGCReport{}, err
	}
	report, err := p.ServerGC(ctx, token, req)
	if errors.Is(err, client.ErrGCUnsupported) {
		return report, errors.New(config.Text("gc.unsupported", info.URL))
	}
	return report, err
}
