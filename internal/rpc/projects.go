package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/session"
)

// ArtifactInvalidator is called when a session's project binding changes.
// Implementations should invalidate cached artifacts for the old project path.
type ArtifactInvalidator interface {
	InvalidateCache(dir string)
}

// ProjectHandler provides native RPC methods for project management.
type ProjectHandler struct {
	pm           *project.ProjectManager
	sessionStore session.Store
	artifactInv  ArtifactInvalidator
}

// NewProjectHandler creates a new project handler.
func NewProjectHandler(pm *project.ProjectManager, store session.Store) *ProjectHandler {
	return &ProjectHandler{pm: pm, sessionStore: store}
}

// SetArtifactInvalidator sets the artifact invalidator called when a session's project changes.
func (h *ProjectHandler) SetArtifactInvalidator(inv ArtifactInvalidator) {
	h.artifactInv = inv
}

// pm returns the ProjectManager or an error if not available.
func (h *ProjectHandler) pmOrErr() (*project.ProjectManager, error) {
	if h.pm == nil {
		return nil, fmt.Errorf("project manager not available")
	}
	return h.pm, nil
}

// RegisterProjectMethods registers all project RPC methods on the server.
func (h *ProjectHandler) RegisterProjectMethods(server *Server) {
	server.RegisterHandler("project.list", h.handleList)
	server.RegisterHandler("project.get", h.handleGet)
	server.RegisterHandler("project.register", h.handleRegister)
	server.RegisterHandler("project.unregister", h.handleUnregister)
	server.RegisterHandler("project.set", h.handleSet)
	server.RegisterHandler("project.sync", h.handleSync)
	server.RegisterHandler("project.status", h.handleStatus)
	server.RegisterHandler("project.detect", h.handleDetect)
}

// handleList handles project.list RPC calls.
func (h *ProjectHandler) handleList(ctx context.Context, params json.RawMessage) (any, error) {
	pm, err := h.pmOrErr()
	if err != nil {
		return nil, err
	}

	projects, err := pm.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	return map[string]any{
		"projects":  projects,
		RPCKeyCount: len(projects),
	}, nil
}

// handleGet handles project.get RPC calls.
func (h *ProjectHandler) handleGet(ctx context.Context, params json.RawMessage) (any, error) {
	pm, err := h.pmOrErr()
	if err != nil {
		return nil, err
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	p, err := pm.Get(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	return p, nil
}

// handleRegister handles project.register RPC calls.
func (h *ProjectHandler) handleRegister(ctx context.Context, params json.RawMessage) (any, error) {
	pm, err := h.pmOrErr()
	if err != nil {
		return nil, err
	}

	var req struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		GitURL    string `json:"git_url"`
		LocalPath string `json:"local_path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	var p *project.Project
	if req.GitURL != "" {
		p, err = pm.RegisterGit(ctx, req.ID, req.Name, req.GitURL)
	} else if req.LocalPath != "" {
		p, err = pm.RegisterLocal(ctx, req.ID, req.Name, req.LocalPath)
	} else {
		return nil, fmt.Errorf("git_url or local_path is required")
	}
	if err != nil {
		return nil, fmt.Errorf("register project: %w", err)
	}

	return p, nil
}

// handleUnregister handles project.unregister RPC calls.
func (h *ProjectHandler) handleUnregister(ctx context.Context, params json.RawMessage) (any, error) {
	pm, err := h.pmOrErr()
	if err != nil {
		return nil, err
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	if err := pm.Unregister(ctx, req.ID); err != nil {
		return nil, fmt.Errorf("unregister project: %w", err)
	}

	return map[string]string{
		RPCKeyStatus: "unregistered",
	}, nil
}

// handleSet handles project.set RPC calls - binds a project to a session.
func (h *ProjectHandler) handleSet(ctx context.Context, params json.RawMessage) (any, error) {
	pm, err := h.pmOrErr()
	if err != nil {
		return nil, err
	}

	var req struct {
		SessionID string `json:"session_id"`
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.SessionID == "" || req.ProjectID == "" {
		return nil, fmt.Errorf("session_id and project_id are required")
	}

	// Verify project exists
	p, err := pm.Get(ctx, req.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}

	// Bind to session
	if h.sessionStore == nil {
		return nil, fmt.Errorf("session store not available")
	}

	// Capture old project path before update so we can invalidate its artifact cache.
	var oldProjectPath string
	if existing := h.sessionStore.Get(req.SessionID); existing != nil {
		oldProjectPath = existing.ProjectPath
	}

	if err := h.sessionStore.SetProject(req.SessionID, req.ProjectID, p.LocalPath); err != nil {
		return nil, fmt.Errorf("set project: %w", err)
	}

	// Invalidate cached artifacts for the old project path.
	if h.artifactInv != nil && oldProjectPath != "" && oldProjectPath != p.LocalPath {
		h.artifactInv.InvalidateCache(oldProjectPath)
	}

	return map[string]any{
		RPCKeyStatus: "bound",
		"session_id": req.SessionID,
		"project_id": req.ProjectID,
	}, nil
}

// handleSync handles project.sync RPC calls.
func (h *ProjectHandler) handleSync(ctx context.Context, params json.RawMessage) (any, error) {
	pm, err := h.pmOrErr()
	if err != nil {
		return nil, err
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	if err := pm.Sync(ctx, req.ID); err != nil {
		return nil, fmt.Errorf("sync project: %w", err)
	}

	return map[string]string{
		RPCKeyStatus: "synced",
	}, nil
}

// handleStatus handles project.status RPC calls.
func (h *ProjectHandler) handleStatus(ctx context.Context, params json.RawMessage) (any, error) {
	pm, err := h.pmOrErr()
	if err != nil {
		return nil, err
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}

	status, err := pm.Status(ctx, req.ID)
	if err != nil {
		return nil, fmt.Errorf("project status: %w", err)
	}
	return status, nil
}

// handleDetect handles project.detect RPC calls.
func (h *ProjectHandler) handleDetect(ctx context.Context, params json.RawMessage) (any, error) {
	pm, err := h.pmOrErr()
	if err != nil {
		return nil, err
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.Path == "" {
		return nil, fmt.Errorf("path is required")
	}

	p, err := pm.DetectFromPath(ctx, req.Path)
	if err != nil {
		return nil, fmt.Errorf("detect project: %w", err)
	}
	return p, nil
}
