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

// ConsolidationStats reports the outcome of a consolidation pass.
type ConsolidationStats struct {
	Processed      int      `json:"processed"`
	Added          int      `json:"added"`
	Skipped        int      `json:"skipped"`
	Duplicates     int      `json:"duplicates"`
	DomainsTouched []string `json:"domains_touched"`
}

// Consolidate reads raw captures from rawCapturesPath, scores each
// trajectory, deduplicates against the existing domain file, and appends
// high-quality examples to {datasetsDir}/{domain}.jsonl. Returns statistics
// about the pass.
func Consolidate(rawCapturesPath, datasetsDir string, minQuality float64, maxDatasetSizeBytes int64) (*ConsolidationStats, error) {
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

		// Per-tool captures (RecordResearch) have empty synthesis by design;
		// only full turn trajectories (RecordTrajectory) are training-worthy.
		if strings.TrimSpace(traj.Synthesis) == "" {
			stats.Skipped++
			continue
		}
		if strings.TrimSpace(traj.Domain) == "" {
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
		stats.DomainsTouched = appendUnique(stats.DomainsTouched, traj.Domain)

		if maxDatasetSizeBytes > 0 {
			if err := enforceRetention(datasetsDir, traj.Domain, maxDatasetSizeBytes); err != nil {
				// Non-fatal: log via stats, continue
				stats.Skipped++
			}
		}
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

// appendUnique appends s to slice if not already present, deduplicating as it
// goes. Returns the (possibly extended) slice.
func appendUnique(slice []string, s string) []string {
	for _, v := range slice {
		if v == s {
			return slice
		}
	}
	return append(slice, s)
}

// enforceRetention trims the oldest entries from {datasetsDir}/{domain}.jsonl
// if its size exceeds maxBytes. "Oldest" = first lines (JSONL is append-only,
// so the head is oldest). It reads all lines, drops the head until under limit,
// rewrites the file. This is O(n) but only runs when the cap is exceeded.
func enforceRetention(datasetsDir, domain string, maxBytes int64) error {
	path := filepath.Join(datasetsDir, domain+".jsonl")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() <= maxBytes {
		return nil
	}

	// Read all lines
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	f.Close()
	if err := sc.Err(); err != nil {
		return err
	}

	// Drop oldest (head) until size is under cap.
	kept := lines
	for len(kept) > 0 {
		size := 0
		for _, l := range kept {
			size += len(l) + 1
		}
		if int64(size) <= maxBytes {
			break
		}
		kept = kept[1:]
	}

	// Atomic rewrite: write temp then rename
	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(out)
	for _, l := range kept {
		w.WriteString(l)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
