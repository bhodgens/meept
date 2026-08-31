package project

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
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
	sessionStore SessionPathUpdater
}

// SessionPathUpdater is the narrow interface needed for bulk session path updates.
type SessionPathUpdater interface {
	UpdateSessionsProjectPath(ctx context.Context, oldPath, newPath string) error
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

// SetSessionStore wires the session store for bulk path updates on rename.
func (pm *ProjectManager) SetSessionStore(s SessionPathUpdater) {
	if s != nil {
		pm.sessionStore = s
	}
}

// validateShortName rejects shorthand project names that contain path
// separators or '..' to prevent directory traversal outside base_dir.
// This mirrors the guard in RegisterGit.
func validateShortName(name string) error {
	if name == "" {
		return fmt.Errorf("project name must not be empty")
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid project name %q: must not contain path separators or '..'", name)
	}
	return nil
}

// RegisterGit clones a git repository and registers it as a project.
// The repo is cloned into baseDir/id.
func (pm *ProjectManager) RegisterGit(ctx context.Context, id, name, gitURL string) (*Project, error) {
	if id == "" {
		id = uuid.New().String()
	}
	// Reject IDs that contain path separators or `..` to prevent path
	// traversal outside BaseDir.
	if strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return nil, fmt.Errorf("invalid project id %q: must not contain path separators or '..'", id)
	}
	// Reject URLs that begin with '-' to prevent option injection into
	// git clone (e.g. "--upload-pack=...").
	if strings.HasPrefix(gitURL, "-") {
		return nil, fmt.Errorf("clone URL %q starts with '-' (refusing ambiguous git arg)", gitURL)
	}

	localPath := filepath.Join(pm.cfg.BaseDir, id)

	// Check if directory already exists
	if _, err := os.Stat(localPath); err == nil {
		// Validate it's actually a git repository before reusing it.
		if _, gitErr := os.Stat(filepath.Join(localPath, ".git")); gitErr != nil {
			return nil, fmt.Errorf("project directory %q already exists but is not a git repository", localPath)
		}
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

	// Dedupe: same path already registered -> return the existing record.
	if existing, err := pm.store.GetProjectByPath(ctx, localPath); err == nil && existing != nil {
		return existing, nil
	}

	if err := pm.store.CreateProject(ctx, p); err != nil {
		return nil, fmt.Errorf("store project: %w", err)
	}
	return p, nil
}

// RegisterLocal registers a local-directory project. Idempotent: if a
// project already exists at the same absolute local_path, it is returned
// instead of creating a duplicate row (name is updated to match).
func (pm *ProjectManager) RegisterLocal(ctx context.Context, id, name, path string) (*Project, error) {
	if id == "" {
		id = uuid.New().String()
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	// Dedupe: same path already registered -> return the existing record.
	existing, err := pm.store.GetProjectByPath(ctx, absPath)
	if err == nil && existing != nil {
		if name != "" && existing.Name != name {
			existing.Name = name
			if err := pm.store.UpdateProject(ctx, existing); err != nil {
				return nil, fmt.Errorf("update project: %w", err)
			}
		}
		return existing, nil
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
			// --left-right: left = commits in origin but not local (behind),
			// right = commits in local but not origin (ahead).
			fmt.Sscanf(parts[0], "%d", &status.Behind)
			fmt.Sscanf(parts[1], "%d", &status.Ahead)
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

// RecentsStore returns the internal recents store for scheduled maintenance.
// Returns nil if recents was not wired during construction.
func (pm *ProjectManager) RecentsStore() *RecentsStore {
	return pm.recentsStore
}

// ---------- sidecar helpers ----------

// sidecarDir is the subdirectory inside a project for meept metadata.
const sidecarDir = ".meept"

// sidecarFile is the filename storing the project's UUID.
const sidecarFile = "project_id"

// sidecarPath returns the full path to the sidecar file for a project directory.
func sidecarPath(projectDir string) string {
	return filepath.Join(projectDir, sidecarDir, sidecarFile)
}

// WriteSidecarID writes the project's UUID into .meept/project_id inside
// the project directory. Creates .meept/ if it doesn't exist.
func (pm *ProjectManager) WriteSidecarID(projectDir, projectID string) error {
	meeptDir := filepath.Join(projectDir, sidecarDir)
	if err := os.MkdirAll(meeptDir, 0o755); err != nil {
		return fmt.Errorf("create sidecar dir: %w", err)
	}
	data := []byte(projectID)
	if err := os.WriteFile(sidecarPath(projectDir), data, 0o644); err != nil {
		return fmt.Errorf("write sidecar: %w", err)
	}
	return nil
}

// ReadSidecarID reads the project UUID from .meept/project_id. Returns
// empty string if the file does not exist (not an error).
func (pm *ProjectManager) ReadSidecarID(projectDir string) (string, error) {
	data, err := os.ReadFile(sidecarPath(projectDir))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read sidecar: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// ---------- active project management ----------

// GetActive returns the currently active project, or nil if none.
func (pm *ProjectManager) GetActive(ctx context.Context) (*Project, error) {
	return pm.store.GetActive(ctx)
}

// DeactivateActive sets all active projects to inactive.
func (pm *ProjectManager) DeactivateActive(ctx context.Context) error {
	return pm.store.DeactivateActive(ctx)
}

// EnsureDefault ensures at least one active project exists. If none exists,
// creates a new project under base_dir/<short-uuid>, initializes git, writes
// the sidecar, and registers it as active. If an active project already
// exists, returns it without creating a new one.
func (pm *ProjectManager) EnsureDefault(ctx context.Context) (*Project, error) {
	// Check if an active project already exists.
	existing, err := pm.GetActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("check active project: %w", err)
	}
	if existing != nil {
		// Verify the active project's directory still exists. If it was
		// removed externally, deactivate it and create a new default.
		if _, statErr := os.Stat(existing.LocalPath); statErr != nil {
			pm.logger.Warn("active project directory missing, creating new default",
				"project_id", existing.ID,
				"path", existing.LocalPath,
				"stat_error", statErr,
			)
			if err := pm.DeactivateActive(ctx); err != nil {
				pm.logger.Warn("failed to deactivate stale project", "error", err)
			}
			// Fall through to create a new default.
		} else {
			return existing, nil
		}
	}

	// Generate a short hex ID (8 chars = 4 bytes, enough for uniqueness
	// within a single user's project directory).
	shortID := pm.generateShortID()
	localPath := filepath.Join(pm.cfg.BaseDir, shortID)

	// Create the directory.
	if err := os.MkdirAll(localPath, 0o755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	// Initialize git.
	if err := pm.runGit(ctx, localPath, "init"); err != nil {
		return nil, fmt.Errorf("git init: %w", err)
	}

	// Set default branch.
	branch := pm.cfg.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	// Configure git to use the desired default branch.
	_ = pm.runGit(ctx, localPath, "symbolic-ref", "HEAD", "refs/heads/"+branch)

	// Configure user if not set (best-effort, ignore errors).
	_ = pm.runGit(ctx, localPath, "config", "user.email", "meept@local")
	_ = pm.runGit(ctx, localPath, "config", "user.name", "meept")

	// Write sidecar.
	if err := pm.WriteSidecarID(localPath, shortID); err != nil {
		return nil, fmt.Errorf("write sidecar: %w", err)
	}

	p := &Project{
		ID:        shortID,
		Name:      shortID,
		Mode:      ModeGit,
		LocalPath: localPath,
		Branch:    branch,
		Status:    "active",
	}

	if err := pm.store.CreateProject(ctx, p); err != nil {
		return nil, fmt.Errorf("create default project: %w", err)
	}

	pm.logger.Info("created default project",
		"id", shortID,
		"path", localPath,
	)

	return p, nil
}

// generateShortID returns an 8-character hex string using crypto/rand.
func (pm *ProjectManager) generateShortID() string {
	b := make([]byte, 4)
	if _, err := crypto_rand.Read(b); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(b)
}

// ---------- project resolution ----------

// CreateOrResolve resolves a user-provided project argument.
//
//   - Absolute path with .git -> DetectFromPath (auto-register or return existing)
//   - Absolute path without .git -> check for sidecar (adopt if found),
//     otherwise RegisterLocal
//   - Shorthand name (not absolute) -> resolve to base_dir/<name>, mkdir -p,
//     git init if no .git, write sidecar, register as git project
//
// For all paths, if a sidecar file exists inside the directory, the project
// is adopted (DB local_path and name updated) rather than re-registered.
func (pm *ProjectManager) CreateOrResolve(ctx context.Context, arg string) (*Project, error) {
	// Check if arg is an absolute path.
	if filepath.IsAbs(arg) {
		absPath, err := filepath.Abs(arg)
		if err != nil {
			return nil, fmt.Errorf("resolve path: %w", err)
		}

		// Check for sidecar — enables adoption of renamed directories.
		sidecarID, err := pm.ReadSidecarID(absPath)
		if err != nil {
			return nil, fmt.Errorf("read sidecar: %w", err)
		}

		if sidecarID != "" {
			// Sidecar exists — look up in DB.
			existing, err := pm.store.GetProject(ctx, sidecarID)
			if err == nil && existing != nil {
				// Adopt: update path and name if they've changed.
				name := filepath.Base(absPath)
				if existing.LocalPath != absPath || existing.Name != name {
					if err := pm.store.UpdateProjectPath(ctx, sidecarID, absPath, name); err != nil {
						return nil, fmt.Errorf("adopt project path: %w", err)
					}
					existing.LocalPath = absPath
					existing.Name = name
				}
				return existing, nil
			}
			// Sidecar exists but project not in DB — re-register with the
			// sidecar's UUID.
			name := filepath.Base(absPath)
			mode := ModeLocal
			if _, err := os.Stat(filepath.Join(absPath, ".git")); err == nil {
				mode = ModeGit
			}
			p := &Project{
				ID:        sidecarID,
				Name:      name,
				Mode:      mode,
				LocalPath: absPath,
				Branch:    pm.cfg.DefaultBranch,
				Status:    "active",
			}
			if p.Branch == "" {
				p.Branch = "main"
			}
			if err := pm.store.CreateProject(ctx, p); err != nil {
				return nil, fmt.Errorf("re-register from sidecar: %w", err)
			}
			return p, nil
		}

		// No sidecar — check for .git.
		if _, err := os.Stat(filepath.Join(absPath, ".git")); err == nil {
			// Git repo — use DetectFromPath (auto-registers or returns existing).
			return pm.DetectFromPath(ctx, absPath)
		}

		// No .git, no sidecar — register as local project.
		name := filepath.Base(absPath)
		return pm.RegisterLocal(ctx, "", name, absPath)
	}

	// Shorthand name: resolve to base_dir/<arg>.
	// Reject names containing path separators or '..' to prevent traversal
	// outside base_dir (same threat model as RegisterGit's id guard).
	if err := validateShortName(arg); err != nil {
		return nil, err
	}
	localPath := filepath.Join(pm.cfg.BaseDir, arg)

	// Check if directory already exists with a sidecar.
	sidecarID, _ := pm.ReadSidecarID(localPath)
	if sidecarID != "" {
		existing, err := pm.store.GetProject(ctx, sidecarID)
		if err == nil && existing != nil {
			return existing, nil
		}
	}

	// Check if already registered by path.
	existing, err := pm.store.GetProjectByPath(ctx, localPath)
	if err == nil && existing != nil {
		return existing, nil
	}

	// Create directory, git init, write sidecar, register.
	if err := os.MkdirAll(localPath, 0o755); err != nil {
		return nil, fmt.Errorf("create project dir: %w", err)
	}

	// Initialize git if .git doesn't exist.
	if _, err := os.Stat(filepath.Join(localPath, ".git")); os.IsNotExist(err) {
		if err := pm.runGit(ctx, localPath, "init"); err != nil {
			return nil, fmt.Errorf("git init: %w", err)
		}
		branch := pm.cfg.DefaultBranch
		if branch == "" {
			branch = "main"
		}
		_ = pm.runGit(ctx, localPath, "symbolic-ref", "HEAD", "refs/heads/"+branch)
		_ = pm.runGit(ctx, localPath, "config", "user.email", "meept@local")
		_ = pm.runGit(ctx, localPath, "config", "user.name", "meept")
	}

	// Determine branch.
	branch, _ := pm.gitOutput(ctx, localPath, "rev-parse", "--abbrev-ref", "HEAD")
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = pm.cfg.DefaultBranch
		if branch == "" {
			branch = "main"
		}
	}

	// Use the sidecar ID if it exists, otherwise generate a new one.
	id := sidecarID
	if id == "" {
		id = pm.generateShortID()
	}

	// Write sidecar.
	if err := pm.WriteSidecarID(localPath, id); err != nil {
		return nil, fmt.Errorf("write sidecar: %w", err)
	}

	p := &Project{
		ID:        id,
		Name:      arg,
		Mode:      ModeGit,
		LocalPath: localPath,
		Branch:    branch,
		Status:    "active",
	}

	if err := pm.store.CreateProject(ctx, p); err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}

	pm.logger.Info("created project from shorthand",
		"id", id,
		"name", arg,
		"path", localPath,
	)

	return p, nil
}

// ---------- rename ----------

// Rename renames a project's directory and updates the DB.
// Only projects whose local_path is under base_dir can be renamed.
// The sidecar file travels with the directory. All sessions pointing
// at the old path are updated to the new path.
func (pm *ProjectManager) Rename(ctx context.Context, projectID, newName string) (*Project, error) {
	p, err := pm.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get project for rename: %w", err)
	}

	// Only allow renaming projects under base_dir.
	baseDir, err := filepath.Abs(pm.cfg.BaseDir)
	if err != nil {
		return nil, fmt.Errorf("resolve base_dir: %w", err)
	}
	projPath, err := filepath.Abs(p.LocalPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}
	if !strings.HasPrefix(projPath, baseDir+string(filepath.Separator)) {
		return nil, fmt.Errorf("cannot rename external project %q: only projects under base_dir can be renamed", p.LocalPath)
	}

	// Reject new names containing path separators or '..' to prevent the
	// renamed directory from escaping base_dir.
	if err := validateShortName(newName); err != nil {
		return nil, fmt.Errorf("invalid new name: %w", err)
	}

	newPath := filepath.Join(baseDir, newName)
	oldPath := p.LocalPath

	// Move the directory.
	if err := os.Rename(p.LocalPath, newPath); err != nil {
		return nil, fmt.Errorf("rename directory: %w", err)
	}

	// Update DB.
	if err := pm.store.UpdateProjectPath(ctx, projectID, newPath, newName); err != nil {
		// Best-effort: try to move the directory back on DB failure.
		if rbErr := os.Rename(newPath, p.LocalPath); rbErr != nil {
			pm.logger.Warn("failed to rollback directory rename after DB error",
				"project_id", projectID,
				"new_path", newPath,
				"old_path", p.LocalPath,
				"db_error", err,
				"rollback_error", rbErr,
			)
		}
		return nil, fmt.Errorf("update project path in DB: %w", err)
	}

	// Update sessions pointing at old path.
	if pm.sessionStore != nil {
		if err := pm.sessionStore.UpdateSessionsProjectPath(ctx, oldPath, newPath); err != nil {
			pm.logger.Warn("failed to update sessions after rename",
				"old_path", oldPath,
				"new_path", newPath,
				"error", err,
			)
		}
	}

	p.LocalPath = newPath
	p.Name = newName

	pm.logger.Info("renamed project",
		"id", projectID,
		"old_path", oldPath,
		"new_path", newPath,
		"new_name", newName,
	)

	return p, nil
}

// SetStatus updates a project's status.
func (pm *ProjectManager) SetStatus(ctx context.Context, id, status string) error {
	p, err := pm.store.GetProject(ctx, id)
	if err != nil {
		return err
	}
	p.Status = status
	return pm.store.UpdateProject(ctx, p)
}

// Config returns the project manager's configuration.
func (pm *ProjectManager) Config() config.ProjectsConfig {
	return pm.cfg
}

// Branch operations are in manager_branches.go
