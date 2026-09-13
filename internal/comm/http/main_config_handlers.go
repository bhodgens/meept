// Package http: read/write endpoints for the main daemon config file
// (meept.json5).
package http

import (
	"errors"
	"net"
	"net/http"

	configCli "github.com/caimlas/meept/internal/config"
)

// mainConfigPayload is the single read result for the main daemon config
// (meept.json5): the absolute path, the raw JSON5 text, and whether the file
// may be overwritten. Every read endpoint serializes this one value, so there
// is exactly one implementation of the read.
type mainConfigPayload struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Writable bool   `json:"writable"`
}

// readMainConfigPayload is THE read path for the main daemon config. It
// resolves the path through config.MainConfigPath() (MEEPT_HOME-aware, never
// os.Getwd) and returns content plus writability; a missing file yields
// Content "" with Writable reflecting the parent directory.
func readMainConfigPayload() (mainConfigPayload, error) {
	content, writable, err := configCli.ReadMainConfig()
	if err != nil {
		return mainConfigPayload{Path: configCli.MainConfigPath()}, err
	}
	return mainConfigPayload{
		Path:     configCli.MainConfigPath(),
		Content:  content,
		Writable: writable,
	}, nil
}

// handleGetMainConfig handles GET /api/v1/config/main.
//
// Returns the absolute path, the raw JSON5 text, and whether the file may be
// overwritten. A missing file yields content "" and writable reflecting the
// parent directory. This is the canonical way to read the main config; there
// is no second read implementation.
//
// Reads are accepted only from loopback clients, exactly like writes: the
// payload is the verbatim meept.json5, which carries transport API keys (and
// provider credentials). The write gate alone left every one of those secrets
// readable — a non-loopback (including a multi-user non-owner) authenticated
// client could GET what it was forbidden to POST. Gate the READ for what the
// READ returns, not for what the write does (audit 2026-09-12, F32).
//
// Consumers on the daemon host (the Flutter GUI, whether shipped as web or
// desktop, and any local tooling) read this endpoint over loopback and are
// unaffected. A REMOTE client is NOT: `transport.http.addr` may bind a
// non-loopback address, and a client whose connection arrives from off-host
// gets 403 here — the same 403 its POST has always gotten, so the meept.json5
// editor is (and was) unusable remotely, and the GUI's read-only multi-user
// probe reports "could not load daemon config" instead of a mode. That is the
// intended trade: the alternative is shipping every api_key to any
// authenticated remote client. Do not weaken this gate for a remote GUI; the
// payload IS the credential file.
//
// NOTE: the menubar app does NOT call this endpoint (it reads /config/client,
// /config/models, /config/agents and /config/menubar only) — it is listed in
// menubar/README.md's table because that table documents the daemon's config
// surface, not the app's call set. Only same-host (loopback) clients use it.
func (s *Server) handleGetMainConfig(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		s.writeError(w, http.StatusForbidden, "config read is restricted to loopback clients")
		return
	}
	if s.configService == nil {
		s.writeError(w, http.StatusServiceUnavailable, "config service not available")
		return
	}

	payload, err := readMainConfigPayload()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, payload)
}

// handleSaveMainConfig handles POST /api/v1/config/main.
//
// The submitted text is validated as JSON5 before anything is written; the
// previous content is copied to <path>.bak and the new content replaces the
// file atomically (temp file + rename) preserving the existing mode.
//
// Writes are accepted only from loopback clients — and so are reads, for the
// same reason (see handleGetMainConfig): the file carries transport API keys,
// so gating only the write left the whole credential set readable.
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
