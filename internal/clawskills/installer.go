// Package clawskills provides the ClawHub registry client for third-party skills.
package clawskills

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstallerConfig holds configuration for the Installer.
type InstallerConfig struct {
	SkillsDir    string // Directory for installed skills (default: ~/.meept/clawskills)
	AutoUpdate   bool   // Auto-update skills on install
	Verify       bool   // Verify signatures (default: true)
}

// DefaultInstallerConfig returns sensible defaults.
func DefaultInstallerConfig() InstallerConfig {
	homeDir, _ := os.UserHomeDir()
	return InstallerConfig{
		SkillsDir:  filepath.Join(homeDir, ".meept", "clawskills"),
		AutoUpdate: false,
		Verify:     true,
	}
}

// Installer handles downloading, verifying, and installing skills from ClawHub.
type Installer struct {
	client   *Client
	index    *Index
	security *SecurityChecker
	config   InstallerConfig
	logger   *slog.Logger
}

// NewInstaller creates a new Installer.
func NewInstaller(client *Client, cfg InstallerConfig, logger *slog.Logger) (*Installer, error) {
	if client == nil {
		client = NewClient()
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Ensure skills directory exists
	if err := os.MkdirAll(cfg.SkillsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create skills directory: %w", err)
	}

	index, err := LoadIndex(cfg.SkillsDir)
	if err != nil {
		// Create new index if not found
		index = NewIndex()
	}

	return &Installer{
		client:   client,
		index:    index,
		security: NewSecurityChecker(),
		config:   cfg,
		logger:   logger,
	}, nil
}

// Install downloads and installs a skill from ClawHub.
func (i *Installer) Install(ctx context.Context, slug string, version string) (*InstalledSkill, error) {
	i.logger.Info("installing skill", "slug", slug, "version", version)

	// Resolve version if not specified
	if version == "" {
		resolved, err := i.client.ResolveVersion(ctx, slug)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve version: %w", err)
		}
		version = resolved.Version
	}

	// Check if already installed at this version
	if existing := i.index.Get(slug); existing != nil && existing.Version == version {
		i.logger.Info("skill already installed at version", "slug", slug, "version", version)
		return existing, nil
	}

	// Get skill details
	skill, err := i.client.SkillDetail(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("failed to get skill details: %w", err)
	}

	// Download archive
	download, err := i.client.Download(ctx, slug, version)
	if err != nil {
		return nil, fmt.Errorf("failed to download skill: %w", err)
	}

	// Verify the download
	if i.config.Verify {
		result := i.security.VerifyDownload(download.Data, download.SHA256, skill.Verified)
		if !result.Valid {
			return nil, fmt.Errorf("verification failed: %v", result.Errors)
		}
		if len(result.Warnings) > 0 {
			for _, w := range result.Warnings {
				i.logger.Warn("security warning", "warning", w)
			}
		}
	}

	// Extract to skills directory
	skillPath := filepath.Join(i.config.SkillsDir, slug)
	if err := i.extractZip(download.Data, skillPath); err != nil {
		return nil, fmt.Errorf("failed to extract skill: %w", err)
	}

	// Verify extracted content
	if i.config.Verify {
		if err := i.security.VerifyExtracted(skillPath); err != nil {
			// Clean up on failure
			os.RemoveAll(skillPath)
			return nil, fmt.Errorf("security check failed: %w", err)
		}
	}

	// Update index
	installed := &InstalledSkill{
		Slug:        slug,
		Name:        skill.Name,
		Version:     version,
		InstalledAt: time.Now(),
		Path:        skillPath,
		SHA256:      download.SHA256,
		AutoUpdate:  i.config.AutoUpdate,
		Verified:    skill.Verified,
	}

	i.index.Set(slug, installed)
	if err := i.index.Save(i.config.SkillsDir); err != nil {
		i.logger.Warn("failed to save index", "error", err)
	}

	i.logger.Info("skill installed", "slug", slug, "version", version, "path", skillPath)
	return installed, nil
}

// Uninstall removes an installed skill.
func (i *Installer) Uninstall(slug string) error {
	installed := i.index.Get(slug)
	if installed == nil {
		return fmt.Errorf("skill not installed: %s", slug)
	}

	// Remove from filesystem
	if err := os.RemoveAll(installed.Path); err != nil {
		return fmt.Errorf("failed to remove skill: %w", err)
	}

	// Update index
	i.index.Delete(slug)
	if err := i.index.Save(i.config.SkillsDir); err != nil {
		i.logger.Warn("failed to save index", "error", err)
	}

	i.logger.Info("skill uninstalled", "slug", slug)
	return nil
}

// Update updates a skill to the latest version.
func (i *Installer) Update(ctx context.Context, slug string) (*InstalledSkill, error) {
	installed := i.index.Get(slug)
	if installed == nil {
		return nil, fmt.Errorf("skill not installed: %s", slug)
	}

	// Resolve latest version
	resolved, err := i.client.ResolveVersion(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve version: %w", err)
	}

	if installed.Version == resolved.Version {
		i.logger.Info("skill already at latest version", "slug", slug, "version", resolved.Version)
		return installed, nil
	}

	// Remove old version and install new
	if err := os.RemoveAll(installed.Path); err != nil {
		i.logger.Warn("failed to remove old version", "error", err)
	}

	return i.Install(ctx, slug, resolved.Version)
}

// UpdateAll updates all installed skills with AutoUpdate enabled.
func (i *Installer) UpdateAll(ctx context.Context) (updated []string, errors []error) {
	for slug, installed := range i.index.Skills {
		if !installed.AutoUpdate {
			continue
		}

		_, err := i.Update(ctx, slug)
		if err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", slug, err))
			continue
		}
		updated = append(updated, slug)
	}
	return
}

// List returns all installed skills.
func (i *Installer) List() []*InstalledSkill {
	return i.index.List()
}

// Get returns an installed skill by slug.
func (i *Installer) Get(slug string) *InstalledSkill {
	return i.index.Get(slug)
}

// extractZip extracts a ZIP archive to the target directory.
func (i *Installer) extractZip(data []byte, targetDir string) error {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("failed to read zip: %w", err)
	}

	// Remove existing directory
	os.RemoveAll(targetDir)

	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("failed to resolve target dir: %w", err)
	}

	for _, file := range reader.File {
		// Security: prevent path traversal
		name := filepath.Clean(file.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			i.logger.Warn("skipping suspicious path in zip", "path", file.Name)
			continue
		}

		targetPath := filepath.Join(targetDir, name)

		// Containment check: resolved target must live under absTargetDir.
		absTarget, aerr := filepath.Abs(targetPath)
		if aerr != nil {
			i.logger.Warn("skipping unresolvable path in zip", "path", file.Name, "error", aerr)
			continue
		}
		if absTarget != absTargetDir && !strings.HasPrefix(absTarget, absTargetDir+string(filepath.Separator)) {
			i.logger.Warn("skipping zip entry outside target dir", "path", file.Name, "resolved", absTarget)
			continue
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
			continue
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}

		// Extract file
		rc, err := file.Open()
		if err != nil {
			return err
		}

		f, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, file.Mode())
		if err != nil {
			rc.Close()
			return err
		}

		_, err = io.Copy(f, rc)
		rc.Close()
		f.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

// Close closes the installer and saves the index.
func (i *Installer) Close() error {
	return i.index.Save(i.config.SkillsDir)
}
