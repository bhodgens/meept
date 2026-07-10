package learning

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LearningMetadata holds provenance and aggregate stats for the learning
// pipeline's data directory. Written to {dataDir}/metadata.json.
type LearningMetadata struct {
	LastConsolidatedAt time.Time            `json:"last_consolidated_at"`
	DomainStats        map[string]DomainStat `json:"domain_stats"`
	RawCapturesCount   int                  `json:"raw_captures_count"`
	SchemaVersion      int                  `json:"schema_version"`
}

// DomainStat summarizes one domain dataset file.
type DomainStat struct {
	ExampleCount int       `json:"example_count"`
	Bytes        int64     `json:"bytes"`
	ModifiedAt   time.Time `json:"modified_at"`
}

// LoadMetadata reads {dataDir}/metadata.json. Missing file = zero-value
// metadata, no error.
func LoadMetadata(dataDir string) (*LearningMetadata, error) {
	path := filepath.Join(dataDir, "metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &LearningMetadata{DomainStats: map[string]DomainStat{}}, nil
		}
		return nil, fmt.Errorf("learning: read metadata: %w", err)
	}
	var m LearningMetadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("learning: parse metadata: %w", err)
	}
	if m.DomainStats == nil {
		m.DomainStats = map[string]DomainStat{}
	}
	return &m, nil
}

// SaveMetadata writes {dataDir}/metadata.json atomically.
func SaveMetadata(dataDir string, m *LearningMetadata) error {
	if m == nil {
		return nil
	}
	if m.DomainStats == nil {
		m.DomainStats = map[string]DomainStat{}
	}
	m.SchemaVersion = 1
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("learning: marshal metadata: %w", err)
	}
	path := filepath.Join(dataDir, "metadata.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("learning: write metadata temp: %w", err)
	}
	return os.Rename(tmp, path)
}

// RefreshDomainStats scans {dataDir}/datasets/*.jsonl and populates DomainStats
// with current counts/sizes. Returns updated metadata.
func RefreshDomainStats(dataDir string, m *LearningMetadata) (*LearningMetadata, error) {
	if m == nil {
		m = &LearningMetadata{}
	}
	datasetsDir := filepath.Join(dataDir, "datasets")
	entries, err := os.ReadDir(datasetsDir)
	if err != nil {
		if os.IsNotExist(err) {
			m.DomainStats = map[string]DomainStat{}
			return m, nil
		}
		return nil, fmt.Errorf("learning: read datasets dir: %w", err)
	}
	stats := map[string]DomainStat{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		domain := strings.TrimSuffix(e.Name(), ".jsonl")
		info, err := e.Info()
		if err != nil {
			continue
		}
		count, _ := countLinesInFile(filepath.Join(datasetsDir, e.Name()))
		stats[domain] = DomainStat{
			ExampleCount: count,
			Bytes:        info.Size(),
			ModifiedAt:   info.ModTime(),
		}
	}
	m.DomainStats = stats
	return m, nil
}

// countLinesInFile counts newline-terminated lines in the file at path.
func countLinesInFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		count++
	}
	return count, scanner.Err()
}
