package services

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/project"
	"github.com/caimlas/meept/internal/session"
)

// ProjectService handles project operations via the service layer.
type ProjectService struct {
	pm    *project.ProjectManager
	store session.Store
}

// NewProjectService creates a project service.
func NewProjectService(pm *project.ProjectManager, store session.Store) *ProjectService {
	return &ProjectService{pm: pm, store: store}
}

// RegisterProjectRequest contains project registration parameters.
type RegisterProjectRequest struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	GitURL    string `json:"git_url,omitempty"`
	LocalPath string `json:"local_path,omitempty"`
}

// SetProjectRequest binds a project to a session.
type SetProjectRequest struct {
	SessionID string `json:"session_id"`
	ProjectID string `json:"project_id"`
}

// ListProjects returns all registered projects.
func (s *ProjectService) ListProjects(ctx context.Context) ([]*project.Project, error) {
	if s.pm == nil {
		return nil, wrapError("project", "List", ErrUnavailable)
	}
	projects, err := s.pm.List(ctx)
	if err != nil {
		return nil, wrapError("project", "List", err)
	}
	return projects, nil
}

// GetProject retrieves a project by ID.
func (s *ProjectService) GetProject(ctx context.Context, id string) (*project.Project, error) {
	if s.pm == nil {
		return nil, wrapError("project", "Get", ErrUnavailable)
	}
	if id == "" {
		return nil, wrapError("project", "Get", ErrInvalidInput)
	}
	p, err := s.pm.Get(ctx, id)
	if err != nil {
		return nil, wrapError("project", "Get", err)
	}
	return p, nil
}

// RegisterProject registers a new project (git or local).
func (s *ProjectService) RegisterProject(ctx context.Context, req RegisterProjectRequest) (*project.Project, error) {
	if s.pm == nil {
		return nil, wrapError("project", "Register", ErrUnavailable)
	}
	if req.Name == "" {
		return nil, wrapError("project", "Register", fmt.Errorf("name is required"))
	}
	if req.GitURL != "" {
		p, err := s.pm.RegisterGit(ctx, req.ID, req.Name, req.GitURL)
		if err != nil {
			return nil, wrapError("project", "Register", err)
		}
		return p, nil
	}
	if req.LocalPath != "" {
		p, err := s.pm.RegisterLocal(ctx, req.ID, req.Name, req.LocalPath)
		if err != nil {
			return nil, wrapError("project", "Register", err)
		}
		return p, nil
	}
	return nil, wrapError("project", "Register", fmt.Errorf("git_url or local_path is required"))
}

// UnregisterProject removes a project by ID.
func (s *ProjectService) UnregisterProject(ctx context.Context, id string) error {
	if s.pm == nil {
		return wrapError("project", "Unregister", ErrUnavailable)
	}
	if id == "" {
		return wrapError("project", "Unregister", ErrInvalidInput)
	}
	if err := s.pm.Unregister(ctx, id); err != nil {
		return wrapError("project", "Unregister", err)
	}
	return nil
}

// SetProject binds a project to a session.
func (s *ProjectService) SetProject(ctx context.Context, req SetProjectRequest) error {
	if s.pm == nil {
		return wrapError("project", "Set", ErrUnavailable)
	}
	if s.store == nil {
		return wrapError("project", "Set", ErrUnavailable)
	}
	if req.SessionID == "" || req.ProjectID == "" {
		return wrapError("project", "Set", fmt.Errorf("session_id and project_id are required"))
	}
	p, err := s.pm.Get(ctx, req.ProjectID)
	if err != nil {
		return wrapError("project", "Set", err)
	}
	if err := s.store.SetProject(req.SessionID, req.ProjectID, p.LocalPath); err != nil {
		return wrapError("project", "Set", err)
	}
	return nil
}

// SyncProject performs a git pull on a project.
func (s *ProjectService) SyncProject(ctx context.Context, id string) error {
	if s.pm == nil {
		return wrapError("project", "Sync", ErrUnavailable)
	}
	if id == "" {
		return wrapError("project", "Sync", ErrInvalidInput)
	}
	if err := s.pm.Sync(ctx, id); err != nil {
		return wrapError("project", "Sync", err)
	}
	return nil
}

// GetProjectStatus returns the runtime status of a project.
func (s *ProjectService) GetProjectStatus(ctx context.Context, id string) (*project.ProjectStatus, error) {
	if s.pm == nil {
		return nil, wrapError("project", "Status", ErrUnavailable)
	}
	if id == "" {
		return nil, wrapError("project", "Status", ErrInvalidInput)
	}
	status, err := s.pm.Status(ctx, id)
	if err != nil {
		return nil, wrapError("project", "Status", err)
	}
	return status, nil
}

// DetectProject detects a project from a filesystem path.
func (s *ProjectService) DetectProject(ctx context.Context, path string) (*project.Project, error) {
	if s.pm == nil {
		return nil, wrapError("project", "Detect", ErrUnavailable)
	}
	if path == "" {
		return nil, wrapError("project", "Detect", ErrInvalidInput)
	}
	p, err := s.pm.DetectFromPath(ctx, path)
	if err != nil {
		return nil, wrapError("project", "Detect", err)
	}
	return p, nil
}

// ListBranches returns all branches for a project.
func (s *ProjectService) ListBranches(ctx context.Context, id string) ([]*project.BranchInfo, error) {
	if s.pm == nil {
		return nil, wrapError("project", "ListBranches", ErrUnavailable)
	}
	if id == "" {
		return nil, wrapError("project", "ListBranches", ErrInvalidInput)
	}
	branches, err := s.pm.ListBranches(ctx, id)
	if err != nil {
		return nil, wrapError("project", "ListBranches", err)
	}
	return branches, nil
}

// CheckoutBranch checks out a branch for a project.
func (s *ProjectService) CheckoutBranch(ctx context.Context, id, branch string) error {
	if s.pm == nil {
		return wrapError("project", "CheckoutBranch", ErrUnavailable)
	}
	if id == "" || branch == "" {
		return wrapError("project", "CheckoutBranch", ErrInvalidInput)
	}
	if err := s.pm.CheckoutBranch(ctx, id, branch); err != nil {
		return wrapError("project", "CheckoutBranch", err)
	}
	return nil
}
