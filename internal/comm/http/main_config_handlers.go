// Package http: read/write endpoints for the main daemon config file
// (meept.json5).
package http

import (
	"errors"
	"net"
	"net/http"

	configCli "github.com/caimlas/meept/internal/config"
)

// handleGetMainConfig handles GET /api/v1/config/main.
//
// Returns the absolute path, the raw JSON5 text, and whether the file may be
// overwritten. A missing file yields content "" and writable reflecting the
// parent directory.
func (s *Server) handleGetMainConfig(w http.ResponseWriter, _ *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	content, writable, err := configCli.ReadMainConfig()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"path":     configCli.MainConfigPath(),
		"content":  content,
		"writable": writable,
	})
}

// handleSaveMainConfig handles POST /api/v1/config/main.
//
// The submitted text is validated as JSON5 before anything is written; the
// previous content is copied to <path>.bak and the new content replaces the
// file atomically (temp file + rename) preserving the existing mode.
// Writes are accepted only from loopback clients.
func (s *Server) handleSaveMainConfig(w http.ResponseWriter, r *http.Request) {
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}
	if !isLoopbackRequest(r) {
		s.writeError(w, http.StatusForbidden, "config write is restricted to loopback clients")
		return
	}

	var body struct {
		Content string `json:"content"`
	}
	if !s.readJSON(w, r, &body) {
		return
	}

	// Validate before touching the filesystem so a bad body cannot clobber
	// the config.
	if err := configCli.ValidateMainConfigJSON5(body.Content); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Reject an unwritable target up front (mode bits / ACLs).
	if _, writable, err := configCli.ReadMainConfig(); err == nil && !writable {
		s.writeError(w, http.StatusForbidden, "main config file is not writable")
		return
	}

	path, err := configCli.WriteMainConfigAtomic(body.Content)
	if err != nil {
		if errors.Is(err, configCli.ErrMainConfigNotWritable) {
			s.writeError(w, http.StatusForbidden, err.Error())
			return
		}
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Log the path only — the config body may contain secrets (api_keys).
	s.logger.Info("main config updated", "path", path)
	s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": path})
}

// isLoopbackRequest reports whether the request's TCP peer is a loopback
// address. RemoteAddr is a "host:port" string set by net/http; the host is
// parsed and validated as an IP with net.ParseIP. Anything that is not
// unambiguously loopback (unparseable, missing port, non-loopback IP) is
// rejected.
func isLoopbackRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
