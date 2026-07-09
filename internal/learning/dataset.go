package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DomainDatasets manages domain-specific JSONL files under baseDir.
type DomainDatasets struct {
	baseDir string
}

// NewDomainDatasets creates a DomainDatasets rooted at baseDir. The directory
// (and parents) are created if they don't exist.
func NewDomainDatasets(baseDir string) (*DomainDatasets, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("learning: baseDir must not be empty")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("learning: mkdir %s: %w", baseDir, err)
	}
	return &DomainDatasets{baseDir: baseDir}, nil
}

// Append writes a TrainingExample as one JSONL line to {baseDir}/{domain}.jsonl.
// The file is opened and closed per call (safe for intermittent use).
func (d *DomainDatasets) Append(domain string, example TrainingExample) error {
	if domain == "" {
		return fmt.Errorf("learning: domain must not be empty")
	}
	filePath := filepath.Join(d.baseDir, domain+".jsonl")
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("learning: open dataset %s: %w", domain, err)
	}
	defer f.Close()

	data, err := json.Marshal(example)
	if err != nil {
		return fmt.Errorf("learning: marshal example: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("learning: write example: %w", err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		return fmt.Errorf("learning: write newline: %w", err)
	}
	return nil
}
