package learning

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ConsolidationStats reports the outcome of a consolidation pass.
type ConsolidationStats struct {
	Processed  int `json:"processed"`
	Added      int `json:"added"`
	Skipped    int `json:"skipped"`
	Duplicates int `json:"duplicates"`
}

// Consolidate reads raw captures from rawCapturesPath, scores each
// trajectory, deduplicates against the existing domain file, and appends
// high-quality examples to {datasetsDir}/{domain}.jsonl. Returns statistics
// about the pass.
func Consolidate(rawCapturesPath, datasetsDir string, minQuality float64) (*ConsolidationStats, error) {
	stats := &ConsolidationStats{}

	f, err := os.Open(rawCapturesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil // nothing to consolidate
		}
		return nil, fmt.Errorf("learning: open raw captures: %w", err)
	}
	defer f.Close()

	datasets, err := NewDomainDatasets(datasetsDir)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		stats.Processed++

		var traj ResearchTrajectory
		if err := json.Unmarshal(line, &traj); err != nil {
			// Skip malformed entries.
			stats.Skipped++
			continue
		}

		score := ScoreExample(traj)
		if score < minQuality {
			stats.Skipped++
			continue
		}

		// Build the training example from the trajectory.
		example := TrainingExample{
			Instruction: traj.Query,
			Input:       "",
			Output:      traj.Synthesis,
			Metadata: ExampleMetadata{
				Source:       "agent_research",
				Domain:       traj.Domain,
				SessionID:    traj.SessionID,
				ToolPath:     toolCallNames(traj.ToolCalls),
				QualityScore: score,
				Timestamp:    time.Now().UTC().Format(time.RFC3339),
			},
		}

		// Deduplicate against the domain file.
		domainFile := filepath.Join(datasetsDir, traj.Domain+".jsonl")
		dup, err := IsDuplicate(example, domainFile)
		if err != nil {
			stats.Skipped++
			continue
		}
		if dup {
			stats.Duplicates++
			continue
		}

		if err := datasets.Append(traj.Domain, example); err != nil {
			stats.Skipped++
			continue
		}
		stats.Added++
	}

	if err := scanner.Err(); err != nil {
		return stats, fmt.Errorf("learning: scan raw captures: %w", err)
	}
	return stats, nil
}

func toolCallNames(calls []ToolCallRecord) []string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Tool
	}
	return names
}
