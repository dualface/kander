package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/dualface/kander/v2/api"
)

func (s *Server) loadAdminToken() error {
	var record struct {
		Token string `json:"token"`
	}
	found, err := readJSON(s.data, api.AdminTokenFile, &record)
	if err != nil {
		return err
	}
	if !found {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		record.Token = hex.EncodeToString(b)
		if err := writeJSON(s.data, api.AdminTokenFile, record); err != nil {
			return err
		}
	}
	b, err := hex.DecodeString(record.Token)
	if err != nil || len(b) != 32 {
		return errors.New("server administration token is damaged")
	}
	s.adminToken = record.Token
	return nil
}

func (s *Server) adminRoutes(mux *routeTable) {
	mux.HandleFunc("GET /api/v1/server", s.authorizedAdmin(func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, api.ServerInfo{
			Version: s.version, PID: os.Getpid(), DataDir: s.data, LogPath: filepath.Join(s.data, "server.log"), Listen: s.listen,
			TTL: api.DurationOf(s.ttl), Renew: api.DurationOf(s.renew), StartedAt: s.startedAt, Projects: len(s.projectInfos()),
		})
	}))
	mux.HandleFunc("POST /api/v1/server/shutdown", s.authorizedAdmin(func(w http.ResponseWriter, r *http.Request) {
		if !s.administrator(r) {
			writeJSONResponse(w, http.StatusForbidden, api.Error{Code: api.CodeInvalidRequest, Message: "server shutdown refused"})
			return
		}
		writeJSONResponse(w, http.StatusAccepted, map[string]string{"status": "stopping"})
		s.shutdownCancel()
	}))
	// Import changes board data, so unlike shutdown it passes the protocol
	// handshake and the readiness gate of domain().
	mux.HandleFunc("POST /api/v1/server/import", s.domain(s.importRoute))
}

func (s *Server) authorizedAdmin(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.authorize(r); err != nil {
			writeJSONResponse(w, err.Status, err)
			return
		}
		fn(w, s.withAttribution(r))
	}
}

// administrator accepts only a loopback peer that presents the private
// administration token. The comparison takes constant time.
func (s *Server) administrator(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return err == nil && ip != nil && ip.IsLoopback() && subtle.ConstantTimeCompare([]byte(r.Header.Get(api.HeaderAdminToken)), []byte(s.adminToken)) == 1
}
