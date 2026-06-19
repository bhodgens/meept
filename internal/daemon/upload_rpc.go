package daemon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/services"
)

// UploadRPCHandler handles file-upload RPC methods. It exposes the
// UploadService to clients (TUI, menubar, web) via the JSON-RPC server so
// that image attachments can be stored and later referenced by upload ID
// in multimodal ChatMessage Parts.
type UploadRPCHandler struct {
	service *services.UploadService
}

// NewUploadRPCHandler creates a new upload RPC handler.
func NewUploadRPCHandler(service *services.UploadService) *UploadRPCHandler {
	return &UploadRPCHandler{service: service}
}

// RegisterUploadMethods registers the upload RPC methods on the given server.
func (h *UploadRPCHandler) RegisterUploadMethods(server *rpc.Server) {
	server.RegisterHandler("upload.upload", h.handleUpload)
	server.RegisterHandler("upload.get", h.handleGet)
}

// uploadRequestParams is the RPC params payload for upload.upload.
type uploadRequestParams struct {
	Data     string `json:"data"`      // base64-encoded file bytes
	Filename string `json:"filename"`  // original filename (used for extension)
	MimeType string `json:"mime_type"` // MIME type (must be in AllowedTypes)
}

// handleUpload accepts a base64-encoded file payload, stores it via the
// UploadService (with SHA-256 dedup), and returns the resulting Upload
// descriptor. Clients use the returned ID to reference the upload in
// ContentPart.ImageRef.URL as "file://<id>".
func (h *UploadRPCHandler) handleUpload(ctx context.Context, params json.RawMessage) (any, error) {
	if h.service == nil {
		return nil, fmt.Errorf("upload service not available")
	}

	var p uploadRequestParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid upload params: %w", err)
	}
	if p.Data == "" {
		return nil, fmt.Errorf("data is required")
	}

	data, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 data: %w", err)
	}

	upload, err := h.service.Upload(ctx, bytes.NewReader(data), p.Filename, p.MimeType)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		rpc.RPCKeyStatus: "ok",
		"upload":         upload,
	}, nil
}

// handleGet retrieves upload metadata by ID. Returns the Upload descriptor
// (without the raw bytes) so clients can render thumbnails or dimensions.
func (h *UploadRPCHandler) handleGet(ctx context.Context, params json.RawMessage) (any, error) {
	if h.service == nil {
		return nil, fmt.Errorf("upload service not available")
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid upload.get params: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	upload, err := h.service.Get(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		rpc.RPCKeyStatus: "ok",
		"upload":         upload,
	}, nil
}
