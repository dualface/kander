package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"github.com/dualface/kander/internal/api"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

func (s *Server) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/server", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResponse(w, http.StatusOK, api.ServerInfo{
			Version: s.version, PID: os.Getpid(), DataDir: s.data, LogPath: filepath.Join(s.data, "server.log"), Listen: s.listen,
			TTL: api.DurationOf(s.ttl), Renew: api.DurationOf(s.renew), StartedAt: s.startedAt, Projects: len(s.projectInfos()),
		})
	})
	mux.HandleFunc("POST /api/v1/server/shutdown", func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || !ip.IsLoopback() || subtle.ConstantTimeCompare([]byte(r.Header.Get(api.HeaderAdminToken)), []byte(s.adminToken)) != 1 {
			writeJSONResponse(w, http.StatusForbidden, api.Error{Code: api.CodeInvalidRequest, Message: "server shutdown refused"})
			return
		}
		writeJSONResponse(w, http.StatusAccepted, map[string]string{"status": "stopping"})
		s.shutdownOnce.Do(func() { close(s.shutdown) })
	})
}
