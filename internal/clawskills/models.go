// Package clawskills provides the ClawHub registry client for third-party skills.
package clawskills

import (
	"time"
)

// Skill represents a skill from the ClawHub registry.
type Skill struct {
	Slug         string            `json:"slug"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Author       string            `json:"author"`
	Version      string            `json:"version"`
	Downloads    int               `json:"downloads"`
	Stars        int               `json:"stars"`
	Tags         []string          `json:"tags"`
	Requirements []string          `json:"requirements"`
	Capabilities []string          `json:"capabilities"`
	Verified     bool              `json:"verified"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// SkillVersion represents a specific version of a skill.
type SkillVersion struct {
	Version     string    `json:"version"`
	SHA256      string    `json:"sha256"`
	ReleaseNote string    `json:"release_note"`
	CreatedAt   time.Time `json:"created_at"`
	Size        int64     `json:"size"`
}

// SearchResult represents a search result from ClawHub.
type SearchResult struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Version     string   `json:"version"`
	Downloads   int      `json:"downloads"`
	Stars       int      `json:"stars"`
	Tags        []string `json:"tags"`
	Verified    bool     `json:"verified"`
	Score       float64  `json:"score"`
}

// DownloadResult represents the result of a skill download.
type DownloadResult struct {
	Data   []byte
	SHA256 string
	Size   int64
}

// ResolveResult represents version resolution result.
type ResolveResult struct {
	Slug         string `json:"slug"`
	Version      string `json:"version"`
	SHA256       string `json:"sha256"`
	DownloadURL  string `json:"download_url"`
	Dependencies []string `json:"dependencies,omitempty"`
}

// InstalledSkill represents a locally installed skill.
type InstalledSkill struct {
	Slug         string    `json:"slug"`
	Name         string    `json:"name"`
	Version      string    `json:"version"`
	InstalledAt  time.Time `json:"installed_at"`
	Path         string    `json:"path"`
	SHA256       string    `json:"sha256"`
	AutoUpdate   bool      `json:"auto_update"`
	Verified     bool      `json:"verified"`
}

// LocalIndex represents the local index of installed skills.
type LocalIndex struct {
	Skills    map[string]*InstalledSkill `json:"skills"`
	UpdatedAt time.Time                  `json:"updated_at"`
	Version   string                     `json:"version"`
}

// VerificationResult represents the result of verifying a skill.
type VerificationResult struct {
	Valid       bool     `json:"valid"`
	SHA256Match bool     `json:"sha256_match"`
	Signed      bool     `json:"signed"`
	Warnings    []string `json:"warnings,omitempty"`
	Errors      []string `json:"errors,omitempty"`
}
