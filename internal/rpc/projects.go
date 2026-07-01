package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/session"
)

// ArtifactInvalidator is called when a session's project binding changes.
// Implementations should invalidate cached artifacts for the old project path.
type ArtifactInvalidator interface {
	InvalidateCache(dir string)
}

// ReadDirRequest is the request for project.readdir RPC.
type ReadDirRequest struct {
	Prefix string `json:"prefix"`
}

// ReadDirResponse is the response for project.readdir RPC.
type ReadDirResponse struct {
	Recents  []string `json:"recents"`
	Matches  []string `json:"matches"`
	GitRoots []string `json:"git_roots"`
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
	server.RegisterHandler("project.readdir", h.handleReadDir)
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

// handleReadDir handles project.readdir RPC calls.
func (h *ProjectHandler) handleReadDir(ctx context.Context, params json.RawMessage) (any, error) {
	pm, err := h.pmOrErr()
	if err != nil {
		return nil, err
	}

	var req ReadDirRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	// Get top 5 recents
	// recentsStore is wired in Task 6 (not yet on ProjectManager).
	// Placeholder: return empty recents until wiring lands.
	var filteredRecents []string
	_ = pm
	// TODO(Task 6): h.pm.recentsStore.ListRecents(ctx, 5) and filter by Req.Prefix

	// Filesystem fallback when no prefix or no recent matches
	var matches, gitRoots []string
	if req.Prefix != "" {
		expanded := expandTilde(req.Prefix)
		entries, err := os.ReadDir(expanded)
		if err == nil {
			for i, entry := range entries {
				if i >= 50 {
					break
				}
				if !entry.IsDir() {
					continue
				}
				path := filepath.Join(expanded, entry.Name())
				matches = append(matches, path)
				gitRoot, _ := findGitRoot(path)
				gitRoots = append(gitRoots, gitRoot)
			}
		}
	}

	return &ReadDirResponse{
		Recents:  filteredRecents,
		Matches:  matches,
		GitRoots: gitRoots,
	}, nil
}

// expandTilde expands ~ to user home directory.
func expandTilde(path string) string {
	if strings.HasPrefix(path, "~") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, strings.TrimPrefix(path[1:], "/"))
	}
	return path
}

// findGitRoot walks up from path looking for .git.
func findGitRoot(path string) (string, error) {
	for {
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			return path, nil
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", fmt.Errorf("no git root found")
		}
		path = parent
	}
}
