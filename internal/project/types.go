package project

import "time"

type Mode string

const (
	ModeGit   Mode = "git"
	ModeLocal Mode = "local"
)

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Mode      Mode      `json:"mode"`
	GitURL    string    `json:"git_url,omitempty"`
	Branch    string    `json:"branch"`
	LocalPath string    `json:"local_path"`
	Status    string    `json:"status"` // "active", "archived", "error"
	LastSync  time.Time `json:"last_sync,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Worktree struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	SessionID string    `json:"session_id,omitempty"`
	PlanID    string    `json:"plan_id,omitempty"`
	Path      string    `json:"path"`
	Branch    string    `json:"branch"`
	Status    string    `json:"status"` // "active", "completed", "cleaned"
	CreatedAt time.Time `json:"created_at"`
}

// ProjectStatus holds runtime status info for a project.
type ProjectStatus struct {
	Branch        string `json:"branch"`
	Dirty         bool   `json:"dirty"`
	Ahead         int    `json:"ahead"`
	Behind        int    `json:"behind"`
	ModifiedFiles int    `json:"modified_files"`
}

