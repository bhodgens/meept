package http

import (
	"encoding/base64"
	"net/http"
	"strings"
)

// handleUploadCreate handles POST /api/v1/uploads.
// Accepts multipart/form-data with a "file" field, or JSON with base64 data.
func (s *Server) handleUploadCreate(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Upload == nil {
		s.writeError(w, http.StatusServiceUnavailable, "upload service not available")
		return
	}

	contentType := r.Header.Get("Content-Type")

	if strings.HasPrefix(contentType, "multipart/form-data") {
		s.handleMultipartUpload(w, r)
		return
	}

	// JSON with base64-encoded data
	if strings.HasPrefix(contentType, "application/json") {
		s.handleJSONUpload(w, r)
		return
	}

	s.writeError(w, http.StatusUnsupportedMediaType, "expected multipart/form-data or application/json")
}

func (s *Server) handleMultipartUpload(w http.ResponseWriter, r *http.Request) {
	// Limit body size
	r.Body = http.MaxBytesReader(w, r.Body, s.services.Upload.MaxSizeBytes())

	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB buffer
		s.writeError(w, http.StatusBadRequest, "failed to parse multipart form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "missing 'file' field")
		return
	}
	defer file.Close()

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	upload, err := s.services.Upload.Upload(r.Context(), file, header.Filename, mimeType)
	if err != nil {
		if strings.Contains(err.Error(), "not allowed") {
			s.writeError(w, http.StatusUnsupportedMediaType, err.Error())
		} else if strings.Contains(err.Error(), "exceeds maximum") {
			s.writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		} else {
			s.writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]any{
		"uploads": []any{upload},
	})
}

func (s *Server) handleJSONUpload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Data     string `json:"data"` // base64-encoded
		Filename string `json:"filename"`
		MimeType string `json:"mime_type"`
	}
	if !s.readJSON(w, r, &req) {
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid base64 data")
		return
	}

	upload, err := s.services.Upload.Upload(r.Context(), strings.NewReader(string(data)), req.Filename, req.MimeType)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.writeJSON(w, http.StatusCreated, map[string]any{
		"uploads": []any{upload},
	})
}

// handleUploadGet handles GET /api/v1/uploads/{id}.
// Returns raw file bytes with the correct Content-Type.
func (s *Server) handleUploadGet(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Upload == nil {
		s.writeError(w, http.StatusServiceUnavailable, "upload service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing upload ID")
		return
	}

	data, mimeType, err := s.services.Upload.Load(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "upload not found")
		return
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(data)
}

// handleUploadMetadata handles GET /api/v1/uploads/{id}/metadata.
func (s *Server) handleUploadMetadata(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Upload == nil {
		s.writeError(w, http.StatusServiceUnavailable, "upload service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing upload ID")
		return
	}

	upload, err := s.services.Upload.Get(r.Context(), id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "upload not found")
		return
	}

	s.writeJSON(w, http.StatusOK, upload)
}

// handleUploadDelete handles DELETE /api/v1/uploads/{id}.
func (s *Server) handleUploadDelete(w http.ResponseWriter, r *http.Request) {
	if s.services == nil || s.services.Upload == nil {
		s.writeError(w, http.StatusServiceUnavailable, "upload service not available")
		return
	}

	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, http.StatusBadRequest, "missing upload ID")
		return
	}

	if err := s.services.Upload.Release(r.Context(), id); err != nil {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]string{"status": "unreferenced"})
}
