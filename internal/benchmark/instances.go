package benchmark

import (
	"encoding/json"
	"fmt"
	"os"
)

// LoadInstances reads benchmark instances from a JSON file.
func LoadInstances(path string) ([]Instance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read instances file: %w", err)
	}

	var instances []Instance
	if err := json.Unmarshal(data, &instances); err != nil {
		return nil, fmt.Errorf("parse instances: %w", err)
	}

	// Validate required fields.
	for i, inst := range instances {
		if inst.ID == "" {
			return nil, fmt.Errorf("instance %d: missing id", i)
		}
		if inst.Repo == "" {
			return nil, fmt.Errorf("instance %q: missing repo", inst.ID)
		}
		if inst.BaseCommit == "" {
			return nil, fmt.Errorf("instance %q: missing base_commit", inst.ID)
		}
		if inst.ProblemStatement == "" {
			return nil, fmt.Errorf("instance %q: missing problem_statement", inst.ID)
		}
	}

	return instances, nil
}

// SaveReport writes a report to a JSON file.
func SaveReport(path string, report *Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}
