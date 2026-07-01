package project

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/google/uuid"
)

// ProjectManager manages project registration, detection, and git operations.
type ProjectManager struct {
	store        *Store
	recentsStore *RecentsStore
	cfg          config.ProjectsConfig
	logger       *slog.Logger
}

// NewProjectManager creates a new ProjectManager.
func NewProjectManager(store *Store, recents *RecentsStore, cfg config.ProjectsConfig, logger *slog.Logger) *ProjectManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProjectManager{
		store:        store,
		recentsStore: recents,
		cfg:          cfg,
		logger:       logger,
	}
}

// RegisterGit clones a git repository and registers it as a project.
// The repo is cloned into baseDir/id.
func (pm *ProjectManager) RegisterGit(ctx context.Context, id, name, gitURL string) (*Project, error) {
	if id == "" {
		id = uuid.New().String()
	}
	// Reject URLs that begin with '-' to prevent option injection into
	// git clone (e.g. "--upload-pack=...").
	if strings.HasPrefix(gitURL, "-") {
		return nil, fmt.Errorf("clone URL %q starts with '-' (refusing ambiguous git arg)", gitURL)
	}

	localPath := filepath.Join(pm.cfg.BaseDir, id)

	// Check if directory already exists
	if _, err := os.Stat(localPath); err == nil {
		pm.logger.Info("project directory already exists, skipping clone", "path", localPath)
	} else {
		// Clone the repo. Use '--' to separate git options from the URL/path.
		if err := pm.runGit(ctx, "", "clone", "--", gitURL, localPath); err != nil {
			return nil, fmt.Errorf("git clone: %w", err)
		}
	}

	// Determine current branch
	branch, _ := pm.gitOutput(ctx, localPath, "rev-parse", "--abbrev-ref", "HEAD")
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = pm.cfg.DefaultBranch
		if branch == "" {
			branch = "main"
		}
	}

	p := &Project{
		ID:        id,
		Name:      name,
		Mode:      ModeGit,
		GitURL:    gitURL,
		Branch:    branch,
		LocalPath: localPath,
		Status:    "active",
	}

	if err := pm.store.CreateProject(ctx, p); err != nil {
		return nil, fmt.Errorf("store project: %w", err)
	}
	return p, nil
}

// RegisterLocal registers a local directory as a project.
func (pm *ProjectManager) RegisterLocal(ctx context.Context, id, name, path string) (*Project, error) {
	if id == "" {
		id = uuid.New().String()
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	p := &Project{
		ID:        id,
		Name:      name,
		Mode:      ModeLocal,
		LocalPath: absPath,
		Status:    "active",
	}

	if err := pm.store.CreateProject(ctx, p); err != nil {
		return nil, fmt.Errorf("store project: %w", err)
	}
	return p, nil
}

// Unregister removes a project from the store. It does not delete files.
func (pm *ProjectManager) Unregister(ctx context.Context, id string) error {
	return pm.store.DeleteProject(ctx, id)
}

// Get retrieves a project by ID.
func (pm *ProjectManager) Get(ctx context.Context, id string) (*Project, error) {
	return pm.store.GetProject(ctx, id)
}

// List returns all registered projects.
func (pm *ProjectManager) List(ctx context.Context) ([]*Project, error) {
	return pm.store.ListProjects(ctx)
}

// DetectFromPath walks up from the given path looking for a .git directory.
// If found, it extracts project info and auto-registers if not already known.
func (pm *ProjectManager) DetectFromPath(ctx context.Context, path string) (*Project, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	// Walk up looking for .git
	gitRoot := ""
	dir := absPath
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			gitRoot = dir
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		dir = parent
	}

	if gitRoot == "" {
		return nil, fmt.Errorf("no git repository found above %s", absPath)
	}

	// Check if already registered by local_path
	existing, err := pm.store.GetProjectByPath(ctx, gitRoot)
	if err == nil && existing != nil {
		return existing, nil
	}

	// Auto-register
	name := filepath.Base(gitRoot)
	gitURL, _ := pm.gitOutput(ctx, gitRoot, "remote", "get-url", "origin")
	gitURL = strings.TrimSpace(gitURL)

	branch, _ := pm.gitOutput(ctx, gitRoot, "rev-parse", "--abbrev-ref", "HEAD")
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "main"
	}

	id := uuid.New().String()
	p := &Project{
		ID:        id,
		Name:      name,
		Mode:      ModeGit,
		GitURL:    gitURL,
		Branch:    branch,
		LocalPath: gitRoot,
		Status:    "active",
	}

	if err := pm.store.CreateProject(ctx, p); err != nil {
		return nil, fmt.Errorf("auto-register project: %w", err)
	}

	pm.logger.Info("auto-detected and registered project",
		"id", id,
		"name", name,
		"path", gitRoot,
	)
	return p, nil
}

// Status returns the runtime git status of a project.
func (pm *ProjectManager) Status(ctx context.Context, id string) (*ProjectStatus, error) {
	p, err := pm.store.GetProject(ctx, id)
	if err != nil {
		return nil, err
	}

	status := &ProjectStatus{}

	// Current branch
	branch, _ := pm.gitOutput(ctx, p.LocalPath, "rev-parse", "--abbrev-ref", "HEAD")
	status.Branch = strings.TrimSpace(branch)

	// Dirty check (porcelain output)
	out, _ := pm.gitOutput(ctx, p.LocalPath, "status", "--porcelain")
	lines := strings.TrimSpace(out)
	if lines != "" {
		status.Dirty = true
		status.ModifiedFiles = len(strings.Split(lines, "\n"))
	}

	// Ahead/behind
	if status.Branch != "" {
		aheadBehind, _ := pm.gitOutput(ctx, p.LocalPath, "rev-list", "--left-right", "--count", "origin/"+status.Branch+"...HEAD")
		parts := strings.Fields(aheadBehind)
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &status.Ahead)
			fmt.Sscanf(parts[1], "%d", &status.Behind)
		}
	}

	return status, nil
}

// Sync performs a git pull on the project.
func (pm *ProjectManager) Sync(ctx context.Context, id string) error {
	p, err := pm.store.GetProject(ctx, id)
	if err != nil {
		return err
	}
	if p.Mode != ModeGit {
		return fmt.Errorf("cannot sync non-git project %s", id)
	}

	if err := pm.runGit(ctx, p.LocalPath, "pull", "--ff-only"); err != nil {
		return fmt.Errorf("git pull: %w", err)
	}

	// Update last_sync
	p.LastSync = pm.now()
	return pm.store.UpdateProject(ctx, p)
}

// now returns the current time in UTC.
func (pm *ProjectManager) now() time.Time {
	return time.Now().UTC()
}

// ---------- git helpers ----------

func (pm *ProjectManager) runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

func (pm *ProjectManager) gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	return string(out), err
}

// TouchRecent updates the recents table for a project path.
func (pm *ProjectManager) TouchRecent(ctx context.Context, path string) error {
	if pm.recentsStore == nil {
		return nil // recents not wired, silently ignore
	}
	return pm.recentsStore.TouchRecent(ctx, path)
}

// ListRecents returns the top N recent project paths.
func (pm *ProjectManager) ListRecents(ctx context.Context, limit int) ([]string, error) {
	if pm.recentsStore == nil {
		return nil, nil
	}
	return pm.recentsStore.ListRecents(ctx, limit)
}

// Branch operations are in manager_branches.go
