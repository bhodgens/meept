package validator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// FilesystemValidator validates file-related evidence.
type FilesystemValidator struct {
	allowedDirs []string // if non-empty, only paths under these dirs are permitted
}

// FilesystemValidatorConfig configures the FilesystemValidator.
type FilesystemValidatorConfig struct {
	// AllowedDirs is an optional list of directory prefixes. When non-empty,
	// any path that does not resolve under one of these directories is rejected.
	AllowedDirs []string
}

// NewFilesystemValidator creates a new FilesystemValidator.
func NewFilesystemValidator() *FilesystemValidator {
	return &FilesystemValidator{}
}

// NewFilesystemValidatorWithConfig creates a new FilesystemValidator with the
// given configuration, including an allowed-directories allowlist.
func NewFilesystemValidatorWithConfig(cfg FilesystemValidatorConfig) *FilesystemValidator {
	v := &FilesystemValidator{allowedDirs: cfg.AllowedDirs}
	return v
}

// isPathAllowed checks that path is within one of the allowed directories.
// If no allowed directories are configured, all paths are permitted.
func (v *FilesystemValidator) isPathAllowed(path string) error {
	if len(v.allowedDirs) == 0 {
		return nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("cannot resolve path: %w", err)
	}
	for _, dir := range v.allowedDirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if absPath == absDir || strings.HasPrefix(absPath, absDir+string(os.PathSeparator)) {
			return nil
		}
	}
	return fmt.Errorf("path %q is outside allowed directories", path)
}

// Validate checks file evidence against the actual filesystem state.
func (v *FilesystemValidator) Validate(ctx context.Context, step *task.TaskStep) ValidationResult {
	var result ValidationResult

	for _, ev := range step.Evidence {
		switch ev.Type {
		case models.EvidenceFileExists:
			if err := v.isPathAllowed(ev.Subject); err != nil {
				result.Errors = append(result.Errors, err.Error())
			} else if err := v.validateFileExists(ev.Subject); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("file not found: %s", ev.Subject))
			}
		case models.EvidenceFileHash:
			if err := v.isPathAllowed(ev.Subject); err != nil {
				result.Errors = append(result.Errors, err.Error())
			} else if err := v.validateFileHash(ev.Subject, ev.Value); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("hash mismatch: %s", ev.Subject))
			}
		default:
			// Not a filesystem evidence type, skip
			continue
		}
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// validateFileExists checks if a file exists at the given path.
func (v *FilesystemValidator) validateFileExists(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return err
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file")
	}
	return nil
}

// validateFileHash verifies a file's SHA256 hash matches the expected value.
func (v *FilesystemValidator) validateFileHash(path, expectedHash string) error {
	actualHash, err := computeSHA256(path)
	if err != nil {
		return err
	}
	if actualHash != expectedHash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	return nil
}

// computeSHA256 computes the SHA256 hash of a file.
func computeSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

// ValidateEvidence validates a single piece of filesystem evidence.
func (v *FilesystemValidator) ValidateEvidence(ctx context.Context, ev models.Evidence) ValidationResult {
	var result ValidationResult

	switch ev.Type {
	case models.EvidenceFileExists:
		if err := v.isPathAllowed(ev.Subject); err != nil {
			result.Errors = append(result.Errors, err.Error())
		} else if err := v.validateFileExists(ev.Subject); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("file not found: %s", ev.Subject))
		}
	case models.EvidenceFileHash:
		if err := v.isPathAllowed(ev.Subject); err != nil {
			result.Errors = append(result.Errors, err.Error())
		} else if err := v.validateFileHash(ev.Subject, ev.Value); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("hash mismatch: %s", ev.Subject))
		}
	default:
		// Not a filesystem evidence type, pass through
	}

	result.Valid = len(result.Errors) == 0
	return result
}
