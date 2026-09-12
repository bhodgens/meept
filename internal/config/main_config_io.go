// Package config: read/write helpers for the main daemon config file
// (meept.json5).
//
// The main config path is resolved through MeeptHome()/MeeptPath() — never
// os.Getwd or a hardcoded ~/.meept — so the MEEPT_HOME override applies.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"
)

// ErrMainConfigNotWritable is returned by WriteMainConfigAtomic when the
// existing main config file exists but is not writable by the current user.
// Callers map it to an HTTP 403 rather than a 500.
var ErrMainConfigNotWritable = errors.New("main config file is not writable")

// MainConfigPath returns the absolute path of the main daemon config file:
// <meept home>/meept.json5 (MEEPT_HOME-aware).
func MainConfigPath() string {
	return MeeptPath("meept.json5")
}

// ReadMainConfig returns the raw JSON5 text of the main config file and
// whether the file may be overwritten. A missing file yields ("", <parent
// directory writable>, nil) so a caller can still create it.
func ReadMainConfig() (string, bool, error) {
	path := MainConfigPath()
	fi, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("stat main config %s: %w", path, err)
		}
		return "", dirWritable(filepath.Dir(path)), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read main config %s: %w", path, err)
	}
	return string(data), fileWritable(path, fi), nil
}

// fileWritable reports whether an existing file can be overwritten. It first
// consults the mode bits (cheap) and then confirms with a real write open so
// ACLs, read-only mounts, and immutable flags are honored.
func fileWritable(path string, fi os.FileInfo) bool {
	if fi.Mode().Perm()&0o200 == 0 {
		return false
	}
	//nolint:gosec // probing writability of a user config file, no data written
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// dirWritable reports whether a file can be created inside dir. It probes by
// creating (and removing) a temp file rather than trusting mode bits.
func dirWritable(dir string) bool {
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return false
	}
	f, err := os.CreateTemp(dir, ".meept-writable-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}

// ValidateMainConfigJSON5 parses content as JSON5 and returns an error when it
// is not valid. hujson accepts comments (// and /* */) and trailing commas but
// requires quoted keys — matching the daemon's own json5 loader.
func ValidateMainConfigJSON5(content string) error {
	if _, err := hujson.Standardize([]byte(content)); err != nil {
		return fmt.Errorf("invalid JSON5: %w", err)
	}
	return nil
}

// WriteMainConfigAtomic validates content, copies the previous file to
// <path>.bak (when one exists), and replaces the file atomically via a temp
// file + rename in the same directory. The existing file mode is preserved;
// a newly created file is written 0600.
//
// It returns the absolute path written. ErrMainConfigNotWritable is returned
// (wrapped) when the existing file is not writable.
func WriteMainConfigAtomic(content string) (string, error) {
	path := MainConfigPath()
	if err := ValidateMainConfigJSON5(content); err != nil {
		return path, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return path, fmt.Errorf("create main config directory %s: %w", dir, err)
	}

	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
		if mode&0o200 == 0 {
			return path, fmt.Errorf("%w: %s", ErrMainConfigNotWritable, path)
		}
		if previous, rerr := os.ReadFile(path); rerr == nil {
			// Preserve the previous content so an operator can recover it.
			if werr := os.WriteFile(path+".bak", previous, 0o600); werr != nil {
				return path, fmt.Errorf("write main config backup %s.bak: %w", path, werr)
			}
		}
	} else if !os.IsNotExist(err) {
		return path, fmt.Errorf("stat main config %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(dir, ".meept-json5-*.tmp")
	if err != nil {
		return path, fmt.Errorf("create temp file for main config: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup on every failure path below.
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		cleanup()
		return path, fmt.Errorf("write temp main config: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return path, fmt.Errorf("set temp main config mode: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return path, fmt.Errorf("sync temp main config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return path, fmt.Errorf("close temp main config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return path, fmt.Errorf("rename main config into place: %w", err)
	}
	return path, nil
}
