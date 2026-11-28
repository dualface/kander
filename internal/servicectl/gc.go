package servicectl

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// ListenRecordName is the file in the data directory where a serving server
// records the address it listens on, so a command that finds the data
// directory lock held can reach a server started with another --listen.
const ListenRecordName = "server-listen.json"

// ListenRecord is the content of ListenRecordName.
type ListenRecord struct {
	Listen string `json:"listen"`
	PID    int    `json:"pid"`
	// Managed reports a server a service manager started (KANDER_MANAGED=1).
	Managed bool `json:"managed,omitempty"`
}

// ReadListenRecord reads the record of dataDir; found is false without a usable one.
func ReadListenRecord(dataDir string) (ListenRecord, bool, error) {
	data, found, err := fs.ReadRegularFileIfExists(filepath.Dir(dataDir), filepath.Join(dataDir, ListenRecordName))
	if err != nil || !found {
		return ListenRecord{}, false, err
	}
	var record ListenRecord
	if json.Unmarshal(data, &record) != nil || record.Listen == "" {
		return ListenRecord{}, false, nil
	}
	return record, true, nil
}

// RecordedURL returns the loopback URL of the address the server of dataDir
// recorded, or "" when there is no usable record. The record can be stale:
// the caller still asks the server at that URL which directory it serves.
func RecordedURL(dataDir string) (string, error) {
	record, found, err := ReadListenRecord(dataDir)
	if err != nil || !found {
		return "", err
	}
	target, err := ProbeURL(record.Listen)
	if err != nil || !Local(target) {
		return "", nil
	}
	return target, nil
}

// ErrNoServer reports that no server answers at the target, so an online
// call cannot be made.
var ErrNoServer = errors.New("no server answers")

// ServerFor returns the info of the loopback server at target when it serves
// dataDir. It returns ErrNoServer when nothing answers at target, and an error
// when the server serves another data directory. A request that timed out
// returns its own error, for which TimedOut is true: something may hold the
// address without answering, which is not the same as no server.
func ServerFor(ctx context.Context, target, dataDir string) (api.ServerInfo, error) {
	if !Local(target) {
		return api.ServerInfo{}, errors.New(config.Text("gc.local_only", target))
	}
	info, err := Info(ctx, target)
	if client.IsUnreachable(err) && !TimedOut(err) {
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

// TimedOut reports an error that a deadline ended: a context deadline or a
// transport timeout.
func TimedOut(err error) bool {
	var timeout net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout()
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

// CheckAdminToken reports whether this user can read the administration token
// of the data directory a local server reported, the way stop and gc read it.
func CheckAdminToken(dataDir string) error {
	_, err := adminToken(dataDir)
	return err
}
