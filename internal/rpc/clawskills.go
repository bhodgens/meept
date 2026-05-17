package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/clawskills"
)

// ClawSkillsHandler handles clawskills RPC methods.
type ClawSkillsHandler struct {
	registry    *clawskills.RegistryClient
	installDir  string
	blockedList []string
}

// NewClawSkillsHandler creates a new clawskills handler.
func NewClawSkillsHandler(registryURL, installDir string, blockedList []string) *ClawSkillsHandler {
	return &ClawSkillsHandler{
		registry:    clawskills.NewRegistryClient(registryURL),
		installDir:  installDir,
		blockedList: blockedList,
	}
}

// RegisterClawSkillsMethods registers all clawskills RPC methods.
func (h *ClawSkillsHandler) RegisterClawSkillsMethods(server *Server) {
	server.RegisterHandler("clawskills.search", h.handleSearch)
	server.RegisterHandler("clawskills.get", h.handleGet)
	server.RegisterHandler("clawskills.install", h.handleInstall)
	server.RegisterHandler("clawskills.list", h.handleList)
	server.RegisterHandler("clawskills.uninstall", h.handleUninstall)
	server.RegisterHandler("clawskills.update", h.handleUpdate)
}

func (h *ClawSkillsHandler) handleSearch(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	skills, err := h.registry.Search(ctx, req.Query)
	if err != nil {
		return nil, err
	}

	// Filter blocked skills
	filtered := make([]clawskills.ClawSkillEntry, 0, len(skills))
	for _, s := range skills {
		if !h.isBlocked(s.Slug) {
			filtered = append(filtered, s)
		}
	}

	return map[string]any{
		"skills": filtered,
		"count":  len(filtered),
	}, nil
}

func (h *ClawSkillsHandler) handleGet(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if req.Slug == "" {
		return nil, fmt.Errorf("slug is required")
	}

	skill, err := h.registry.Get(ctx, req.Slug)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"skill": skill,
	}, nil
}

func (h *ClawSkillsHandler) handleInstall(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if req.Slug == "" {
		return nil, fmt.Errorf("slug is required")
	}

	if h.isBlocked(req.Slug) {
		return nil, fmt.Errorf("clawskill %s is blocked by administrator", req.Slug)
	}

	installed, err := h.registry.Install(ctx, req.Slug, h.installDir)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"status":       "installed",
		"slug":         installed.Slug,
		"name":         installed.Name,
		"version":      installed.Version,
		"install_path": installed.InstallPath,
	}, nil
}

func (h *ClawSkillsHandler) handleList(ctx context.Context, params json.RawMessage) (any, error) {
	installed, err := clawskills.ListInstalled(h.installDir)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"skills": installed,
		"count":  len(installed),
	}, nil
}

func (h *ClawSkillsHandler) handleUninstall(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if req.Slug == "" {
		return nil, fmt.Errorf("slug is required")
	}

	if err := clawskills.Uninstall(h.installDir, req.Slug); err != nil {
		return nil, err
	}

	return map[string]any{
		"status": "uninstalled",
		"slug":   req.Slug,
	}, nil
}

func (h *ClawSkillsHandler) handleUpdate(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	if req.Slug == "" {
		return nil, fmt.Errorf("slug is required")
	}

	if h.isBlocked(req.Slug) {
		return nil, fmt.Errorf("clawskill %s is blocked by administrator", req.Slug)
	}

	installed, err := h.registry.Update(ctx, req.Slug, h.installDir)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"status":       "updated",
		"slug":         installed.Slug,
		"name":         installed.Name,
		"version":      installed.Version,
		"install_path": installed.InstallPath,
	}, nil
}

func (h *ClawSkillsHandler) isBlocked(slug string) bool {
	normalized := clawskills.NormalizeSlug(slug)
	for _, blocked := range h.blockedList {
		if blocked == normalized || blocked == slug {
			return true
		}
	}
	return false
}
