// Package clawskills provides third-party skill registry and installation for meept.
// ClawSkills are community-contributed skills that can be discovered, installed, and managed.
package clawskills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	// DefaultRegistryURL is the default clawskills registry endpoint.
	DefaultRegistryURL = "https://registry.meept.dev/api/v1/clawskills"

	// ClawSkillPrefix is the prefix for clawskill slugs.
	ClawSkillPrefix = "claw:"

	// maxArchiveSize is the maximum size for a downloaded skill archive (50 MB).
	maxArchiveSize int64 = 50 * 1024 * 1024
)

// validSlugRe matches only alphanumeric characters, hyphens, and underscores.
var validSlugRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ClawSkillEntry represents a skill in the registry.
type ClawSkillEntry struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Repository  string   `json:"repository"`
	RiskLevel   string   `json:"risk_level"`
	Tags        []string `json:"tags"`
	Requires    []string `json:"requires"`
	DownloadURL string   `json:"download_url"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// ClawSkillInstalled represents an installed clawskill.
type ClawSkillInstalled struct {
	ClawSkillEntry
	InstalledAt string `json:"installed_at"`
	InstallPath string `json:"install_path"`
	Enabled     bool   `json:"enabled"`
}

// RegistryClient provides methods to interact with the clawskills registry.
type RegistryClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewRegistryClient creates a new registry client.
func NewRegistryClient(registryURL string) *RegistryClient {
	if registryURL == "" {
		registryURL = DefaultRegistryURL
	}
	return &RegistryClient{
		baseURL: registryURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Search searches for clawskills matching a query.
func (c *RegistryClient) Search(ctx context.Context, query string) ([]ClawSkillEntry, error) {
	safeQuery := url.QueryEscape(query)
	reqURL := fmt.Sprintf("%s/search?q=%s", c.baseURL, safeQuery)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned %d", resp.StatusCode)
	}

	var result struct {
		Skills []ClawSkillEntry `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Skills, nil
}

// Get fetches details for a specific clawskill.
func (c *RegistryClient) Get(ctx context.Context, slug string) (*ClawSkillEntry, error) {
	if !validateSlug(slug) {
		return nil, fmt.Errorf("invalid slug: %q", slug)
	}
	reqURL := fmt.Sprintf("%s/skills/%s", c.baseURL, url.PathEscape(slug))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("clawskill not found: %s", slug)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned %d", resp.StatusCode)
	}

	var result struct {
		Skill ClawSkillEntry `json:"skill"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result.Skill, nil
}

// Install downloads and installs a clawskill.
func (c *RegistryClient) Install(ctx context.Context, slug, destDir string) (*ClawSkillInstalled, error) {
	// Validate slug to prevent path traversal.
	if !validateSlug(slug) {
		return nil, fmt.Errorf("invalid slug: %q", slug)
	}

	// Get skill details
	skill, err := c.Get(ctx, slug)
	if err != nil {
		return nil, err
	}

	// Create install directory
	skillDir := filepath.Join(destDir, slug)

	// Verify the resolved path stays within destDir.
	if !isWithinDir(destDir, skillDir) {
		return nil, fmt.Errorf("slug %q escapes install directory", slug)
	}

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create install directory: %w", err)
	}

	// Download skill archive
	archiveURL := skill.DownloadURL
	if archiveURL == "" {
		archiveURL = fmt.Sprintf("%s/skills/%s/download", c.baseURL, url.PathEscape(slug))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, http.NoBody)
	if err != nil {
		os.RemoveAll(skillDir)
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		os.RemoveAll(skillDir)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.RemoveAll(skillDir)
		return nil, fmt.Errorf("download failed: %d", resp.StatusCode)
	}

	// Read and extract archive (simplified - in production would handle tar.gz)
	archiveData, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveSize))
	if err != nil {
		os.RemoveAll(skillDir)
		return nil, err
	}

	// For now, save as a simple manifest (production would extract archive)
	manifest := map[string]any{
		"slug":         slug,
		"name":         skill.Name,
		"version":      skill.Version,
		"installed_at": time.Now().Format(time.RFC3339),
		"source":       archiveURL,
	}
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(filepath.Join(skillDir, "manifest.json"), manifestData, 0o644); err != nil {
		os.RemoveAll(skillDir)
		return nil, err
	}

	// Save raw archive for extraction (production would extract)
	if err := os.WriteFile(filepath.Join(skillDir, "skill.tar.gz"), archiveData, 0o644); err != nil {
		os.RemoveAll(skillDir)
		return nil, err
	}

	return &ClawSkillInstalled{
		ClawSkillEntry: *skill,
		InstalledAt:    time.Now().Format(time.RFC3339),
		InstallPath:    skillDir,
		Enabled:        true,
	}, nil
}

// ListInstalled lists all installed clawskills.
func ListInstalled(installDir string) ([]ClawSkillInstalled, error) {
	var installed []ClawSkillInstalled

	entries, err := os.ReadDir(installDir)
	if err != nil {
		if os.IsNotExist(err) {
			return installed, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(installDir, entry.Name(), "manifest.json")
		manifestData, err := os.ReadFile(manifestPath)
		if err != nil {
			continue // Skip invalid installations
		}

		var manifest map[string]any
		if err := json.Unmarshal(manifestData, &manifest); err != nil {
			continue
		}

		slug, _ := manifest["slug"].(string)
		if slug == "" {
			slug = entry.Name()
		}

		installed = append(installed, ClawSkillInstalled{
			ClawSkillEntry: ClawSkillEntry{
				Slug:        slug,
				Name:        getStringOr(manifest, "name", entry.Name()),
				Version:     getStringOr(manifest, "version", "unknown"),
				Description: getStringOr(manifest, "description", ""),
			},
			InstalledAt: getStringOr(manifest, "installed_at", ""),
			InstallPath: filepath.Join(installDir, entry.Name()),
			Enabled:     true,
		})
	}

	return installed, nil
}

// Uninstall removes an installed clawskill.
func Uninstall(installDir, slug string) error {
	// Validate slug to prevent path traversal.
	if !validateSlug(slug) {
		return fmt.Errorf("invalid slug: %q", slug)
	}
	skillDir := filepath.Join(installDir, slug)

	// Verify the resolved path stays within installDir.
	if !isWithinDir(installDir, skillDir) {
		return fmt.Errorf("slug %q escapes install directory", slug)
	}

	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("failed to remove skill: %w", err)
	}
	return nil
}

// Update checks for updates and updates a clawskill.
func (c *RegistryClient) Update(ctx context.Context, slug, destDir string) (*ClawSkillInstalled, error) {
	// Uninstall old version first
	if err := Uninstall(destDir, slug); err != nil {
		return nil, err
	}

	// Install new version
	return c.Install(ctx, slug, destDir)
}

func getStringOr(m map[string]any, key, defaultVal string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return defaultVal
}

// validateSlug checks that a slug contains only safe characters.
func validateSlug(slug string) bool {
	return validSlugRe.MatchString(slug)
}

// isWithinDir reports whether target resolves inside dir.
func isWithinDir(dir, target string) bool {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absTarget, absDir+string(os.PathSeparator)) || absTarget == absDir
}

// Ensure slug has claw: prefix
func normalizeSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if !strings.HasPrefix(slug, ClawSkillPrefix) {
		return ClawSkillPrefix + slug
	}
	return slug
}

// Remove claw: prefix for display
func DisplaySlug(slug string) string {
	return strings.TrimPrefix(slug, ClawSkillPrefix)
}

// NormalizeSlug ensures slug has claw: prefix
func NormalizeSlug(slug string) string {
	return normalizeSlug(slug)
}
