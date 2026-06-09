//go:build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/magefile/mage/sh"
)

const gomarkdocPkg = "github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest"

// DocsGenerate runs gomarkdoc over key internal packages and outputs
// markdown documentation to docs/reference/generated/. This replaces
// the legacy cmd/gendoc tool with a godoc-comment-driven approach
// that is better suited for the MkDocs-based documentation site.
//
// Packages documented:
//   - internal/config      (configuration schema)
//   - internal/agent       (agent loop and orchestration)
//   - internal/bus         (pub/sub message bus)
//   - internal/security    (security engine, sanitizer, tirith)
//   - internal/llm         (LLM client and provider resolution)
//   - internal/memory      (episodic, task, and graph memory)
//   - internal/tools       (tool registry and builtins)
//   - internal/skills      (skill discovery and execution)
//   - internal/scheduler   (cron-based job scheduling)
func DocsGenerate() error {
	const outputDir = "docs/reference/generated"

	// Determine module root by walking up from this file to find go.mod.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("cannot determine magefile location")
	}
	root := filepath.Dir(filepath.Dir(filename))

	// Map of Go package import paths to output filenames.
	// Keys are package paths relative to the module root.
	// Values are the markdown file names under outputDir.
	packages := map[string]string{
		"github.com/caimlas/meept/internal/config":    "config.md",
		"github.com/caimlas/meept/internal/agent":     "agent.md",
		"github.com/caimlas/meept/internal/bus":       "bus.md",
		"github.com/caimlas/meept/internal/security":  "security.md",
		"github.com/caimlas/meept/internal/llm":       "llm.md",
		"github.com/caimlas/meept/internal/memory":    "memory.md",
		"github.com/caimlas/meept/internal/tools":     "tools.md",
		"github.com/caimlas/meept/internal/skills":    "skills.md",
		"github.com/caimlas/meept/internal/scheduler": "scheduler.md",
	}

	// Ensure gomarkdoc is available. If not in PATH, install it to GOPATH/bin.
	gomarkdocBin, err := resolveGomarkdoc()
	if err != nil {
		return fmt.Errorf("gomarkdoc setup failed: %w", err)
	}

	// Ensure output directory exists.
	outDir := filepath.Join(root, outputDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil { //nolint:gosec // docs directory is intentionally world-readable
		return fmt.Errorf("failed to create output directory %s: %w", outDir, err)
	}

	for pkg, outFile := range packages {
		dest := filepath.Join(outDir, outFile)
		fmt.Printf("Generating %s from %s\n", outFile, pkg)

		cmd := exec.Command(gomarkdocBin, "--output", dest, "--format", "plain", pkg)
		cmd.Dir = root

		if out, err := cmd.CombinedOutput(); err != nil {
			// gomarkdoc may fail on packages with no exported symbols;
			// log a warning and continue rather than aborting.
			fmt.Fprintf(os.Stderr, "warning: gomarkdoc failed for %s: %v\n%s\n", pkg, err, out)
			continue
		}
	}

	// Generate index.md with a table of contents pointing to each generated file.
	index := `# Generated Package Documentation

This directory contains documentation auto-generated from Go source code using [gomarkdoc](https://github.com/princjef/gomarkdoc).

## Packages

| Package | Description |
|---------|-------------|
`
	for pkg, outFile := range packages {
		index += fmt.Sprintf("| [%s](%s) | %s |\n", filepath.Base(pkg), outFile, pkg)
	}

	if err := os.WriteFile(filepath.Join(outDir, "index.md"), []byte(index), 0o644); err != nil { //nolint:gosec // docs file
		return fmt.Errorf("failed to write index.md: %w", err)
	}

	// Update .nav.yml with the generated files.
	nav := "nav:\n"
	for _, outFile := range packages {
		nav += fmt.Sprintf("  - %s\n", outFile)
	}
	nav += "  - index.md\n"
	if err := os.WriteFile(filepath.Join(outDir, ".nav.yml"), []byte(nav), 0o644); err != nil { //nolint:gosec // config file
		return fmt.Errorf("failed to write .nav.yml: %w", err)
	}

	fmt.Printf("Done. Generated %d documentation files in %s\n", len(packages)+1, outputDir)
	return nil
}

// DocsCheck verifies that generated docs are up-to-date by comparing
// them against freshly generated output. Returns an error if any file
// differs, suitable for use in CI.
func DocsCheck() error {
	const outputDir = "docs/reference/generated"

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("cannot determine magefile location")
	}
	root := filepath.Dir(filepath.Dir(filename))

	// Ensure gomarkdoc is available.
	_, err := exec.LookPath("gomarkdoc")
	if err != nil {
		return fmt.Errorf("gomarkdoc not found in PATH; run 'go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest'")
	}

	packages := []struct {
		pkg     string
		outFile string
	}{
		{"github.com/caimlas/meept/internal/config", "config.md"},
		{"github.com/caimlas/meept/internal/agent", "agent.md"},
		{"github.com/caimlas/meept/internal/bus", "bus.md"},
		{"github.com/caimlas/meept/internal/security", "security.md"},
		{"github.com/caimlas/meept/internal/llm", "llm.md"},
		{"github.com/caimlas/meept/internal/memory", "memory.md"},
		{"github.com/caimlas/meept/internal/tools", "tools.md"},
		{"github.com/caimlas/meept/internal/skills", "skills.md"},
		{"github.com/caimlas/meept/internal/scheduler", "scheduler.md"},
	}

	stale := false
	for _, p := range packages {
		dest := filepath.Join(root, outputDir, p.outFile)
		cmd := exec.Command("gomarkdoc", "--output", dest, "--format", "plain", "--check", p.pkg)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			stale = true
			fmt.Fprintf(os.Stderr, "STALE: %s (%s)\n%s\n", p.outFile, p.pkg, out)
		}
	}

	if stale {
		return fmt.Errorf("generated docs are stale; run 'mage docsGenerate' to regenerate")
	}
	fmt.Println("All generated docs are up-to-date.")
	return nil
}

// DocsClean removes all generated documentation files.
func DocsClean() error {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("cannot determine magefile location")
	}
	root := filepath.Dir(filepath.Dir(filename))
	outDir := filepath.Join(root, "docs", "reference", "generated")

	entries, err := os.ReadDir(outDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read generated dir: %w", err)
	}

	for _, entry := range entries {
		if entry.Name() == ".nav.yml" {
			continue
		}
		if err := os.Remove(filepath.Join(outDir, entry.Name())); err != nil {
			return fmt.Errorf("failed to remove %s: %w", entry.Name(), err)
		}
		fmt.Printf("Removed %s\n", entry.Name())
	}

	fmt.Println("Cleaned generated documentation files.")
	return nil
}

// DocsInstall ensures gomarkdoc is available in GOPATH/bin.
func DocsInstall() error {
	_, err := resolveGomarkdoc()
	return err
}

// resolveGomarkdoc finds or installs the gomarkdoc binary.
// It first checks PATH, then falls back to GOPATH/bin, and finally
// installs it via "go install" if neither exists.
func resolveGomarkdoc() (string, error) {
	// Check PATH first.
	if bin, err := exec.LookPath("gomarkdoc"); err == nil {
		return bin, nil
	}

	// Check GOPATH/bin explicitly (may not be in PATH).
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine GOPATH: %w", err)
		}
		gopath = filepath.Join(home, "go")
	}
	candidate := filepath.Join(gopath, "bin", "gomarkdoc")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	// Install it.
	fmt.Println("Installing gomarkdoc...")
	if err := sh.RunV("go", "install", gomarkdocPkg); err != nil {
		return "", fmt.Errorf("failed to install gomarkdoc: %w", err)
	}
	return candidate, nil
}
