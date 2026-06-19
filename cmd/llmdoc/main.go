// Command llmdoc generates a single flat-text document concatenating all
// hand-written Meept documentation for consumption by LLMs. The output
// is written to docs/generated/llms-readme-full.txt.
//
// Usage:
//
//	go run ./cmd/llmdoc
//	go run ./cmd/llmdoc -output path/to/output.txt
package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// docSection defines a top-level section in the output document.
type docSection struct {
	title    string
	dir      string // relative to docs/
	skipDirs bool   // if true, only include .md files directly in dir, not subdirs
}

// sections defines the canonical reading order for the flat document.
var sections = []docSection{
	{title: "Overview", dir: "", skipDirs: true},
	{title: "Getting Started", dir: "getting-started"},
	{title: "Concepts", dir: "concepts"},
	{title: "Configuration", dir: "configuration"},
	{title: "Workflows", dir: "workflows"},
	{title: "Reference", dir: "reference"},
}

// filesToSkip lists filenames that should be excluded from the output.
var filesToSkip = map[string]bool{
	"README.md": true,
}

// topLevelSkipPatterns are filename patterns to exclude from the root docs/
// directory (these are historical plans, audits, and worklists that don't
// belong in user-facing LLM documentation).
var topLevelSkipPatterns = []string{
	"plan-", "audit-", "2026", "WORKLIST", "review-", "implementation-",
	"alternate-", "bugs-", "feature-", "test-plan-", "research-",
	"claude-", "ouroboros-", "requirements",
}

func main() {
	docsDir := flag.String("docs", "docs", "Path to the docs directory")
	outputPath := flag.String("output", "docs/generated/llms-readme-full.txt", "Output file path")
	flag.Parse()

	absDocsDir, err := filepath.Abs(*docsDir)
	if err != nil {
		fail("failed to resolve docs path: %v", err)
	}

	if _, err := os.Stat(absDocsDir); os.IsNotExist(err) {
		fail("docs directory not found: %s", absDocsDir)
	}

	var b strings.Builder

	// --- Header ---
	writeHeader(&b)

	// --- Table of Contents ---
	writeTOC(&b, absDocsDir)

	// --- Body: each section ---
	for _, section := range sections {
		writeSection(&b, absDocsDir, section)
	}

	// --- Footer ---
	writeFooter(&b)

	// Ensure output directory exists
	outputDir := filepath.Dir(*outputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		fail("failed to create output directory %s: %v", outputDir, err)
	}

	if err := os.WriteFile(*outputPath, []byte(b.String()), 0o644); err != nil {
		fail("failed to write output: %v", err)
	}

	fmt.Printf("Generated %s (%s, %d sections)\n", *outputPath, formatBytes(int64(b.Len())), len(sections))
}

func writeHeader(b *strings.Builder) {
	b.WriteString("# Meept — Full Documentation for LLMs\n\n")
	b.WriteString("This document is a complete flattening of all Meept documentation into a single ")
	b.WriteString("text file, designed to be fed to LLMs as context. It covers installation, ")
	b.WriteString("architecture, configuration, workflows, and API reference.\n\n")
	b.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().UTC().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("Source: https://github.com/caimlas/meept\n\n"))
	b.WriteString("---\n\n")
}

func writeTOC(b *strings.Builder, docsDir string) {
	b.WriteString("## Table of Contents\n\n")
	for _, section := range sections {
		dirPath := filepath.Join(docsDir, section.dir)
		files := collectMarkdownFiles(dirPath, section.skipDirs)
		if section.dir == "" {
			b.WriteString(fmt.Sprintf("- %s\n", section.title))
			for _, f := range files {
				heading := titleFromFile(f)
				b.WriteString(fmt.Sprintf("  - %s\n", heading))
			}
			continue
		}
		if len(files) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s\n", section.title))
		for _, f := range files {
			heading := titleFromFile(f)
			b.WriteString(fmt.Sprintf("  - %s\n", heading))
		}
	}
	b.WriteString("\n---\n\n")
}

func writeSection(b *strings.Builder, docsDir string, section docSection) {
	dirPath := filepath.Join(docsDir, section.dir)
	files := collectMarkdownFiles(dirPath, section.skipDirs)

	if len(files) == 0 {
		return
	}

	b.WriteString(fmt.Sprintf("# %s\n\n", section.title))

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping %s: %v\n", file, err)
			continue
		}

		// Compute a relative label for the source path
		relPath, _ := filepath.Rel(docsDir, file)

		heading := titleFromFile(file)
		b.WriteString(fmt.Sprintf("## %s\n\n", heading))
		b.WriteString(fmt.Sprintf("> Source: `%s`\n\n", relPath))

		// Strip the first H1 heading from the content to avoid duplication,
		// since we already wrote the section/heading above.
		body := stripFirstH1(string(content))
		b.WriteString(body)

		// Ensure content ends with newline separation
		if !strings.HasSuffix(body, "\n\n") {
			b.WriteString("\n")
		}
		b.WriteString("---\n\n")
	}
}

func writeFooter(b *strings.Builder) {
	b.WriteString("\n---\n\n")
	b.WriteString("_End of document._\n")
}

// collectMarkdownFiles returns sorted .md files in a directory.
// If topLevelOnly is true, only files directly in dir are returned
// (no subdirectory recursion), and topLevelSkipPatterns are applied.
func collectMarkdownFiles(dir string, topLevelOnly bool) []string {
	var files []string

	if topLevelOnly {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			if skip := filesToSkip[entry.Name()]; skip {
				continue
			}
			if shouldSkipTopLevel(entry.Name()) {
				continue
			}
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	} else {
		filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			if d.Name() == "README.md" {
				return nil
			}
			files = append(files, path)
			return nil
		})
	}

	sort.Strings(files)
	return files
}

// titleFromFile extracts a human-readable title from a markdown file path.
// It uses the first H1 heading if present, otherwise falls back to the
// filename.
func titleFromFile(path string) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return prettifyFilename(filepath.Base(path))
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			title := strings.TrimPrefix(line, "# ")
			title = strings.TrimSpace(title)
			// Skip generic "index" titles
			if title == "index" || title == "Index" {
				return prettifyFilename(filepath.Base(path))
			}
			return title
		}
	}
	return prettifyFilename(filepath.Base(path))
}

func prettifyFilename(name string) string {
	name = strings.TrimSuffix(name, ".md")
	name = strings.ReplaceAll(name, "-", " ")
	if name == "index" {
		return "Overview"
	}
	return strings.Title(name) //nolint:staticcheck // simple title casing for filenames
}

// stripFirstH1 removes the first "# " heading line from the content.
// This prevents duplicate headings since writeSection already emits one.
func stripFirstH1(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	headingRemoved := false

	for _, line := range lines {
		if !headingRemoved && strings.HasPrefix(strings.TrimSpace(line), "# ") {
			headingRemoved = true
			continue
		}
		result = append(result, line)
	}

	// Trim leading blank lines left after removing the heading
	out := strings.Join(result, "\n")
	out = strings.TrimLeft(out, "\n")
	return out
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "llmdoc: "+format+"\n", args...)
	os.Exit(1)
}

// shouldSkipTopLevel returns true if the filename matches one of the
// topLevelSkipPatterns (historical plans, audits, worklists, etc.).
func shouldSkipTopLevel(name string) bool {
	for _, pattern := range topLevelSkipPatterns {
		if strings.HasPrefix(name, pattern) {
			return true
		}
	}
	return false
}
