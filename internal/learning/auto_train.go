package learning

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// AutoTrainRecord tracks the last auto-train attempt for a domain so we do
// not re-trigger training on every consolidate pass with the same data size.
type AutoTrainRecord struct {
	ExampleCount int       `json:"example_count"`
	TrainedAt    time.Time `json:"trained_at"`
	Model        string    `json:"model,omitempty"`
	Status       string    `json:"status,omitempty"` // "started", "completed", "failed"
}

// DomainsReadyForTrain returns domain names whose current example count is
// at or above threshold and that have grown since the last successful
// auto-train (or were never auto-trained). Domains are sorted alphabetically.
//
// threshold <= 0 means "never ready".
func DomainsReadyForTrain(meta *LearningMetadata, threshold int) []string {
	if meta == nil || threshold <= 0 || len(meta.DomainStats) == 0 {
		return nil
	}
	var ready []string
	for domain, ds := range meta.DomainStats {
		if ds.ExampleCount < threshold {
			continue
		}
		if last, ok := meta.LastAutoTrain[domain]; ok {
			if ds.ExampleCount <= last.ExampleCount && last.Status == "completed" {
				continue
			}
			if last.Status == "started" && ds.ExampleCount == last.ExampleCount {
				continue
			}
		}
		ready = append(ready, domain)
	}
	sort.Strings(ready)
	return ready
}

// MarkAutoTrainStarted records that auto-train began for domain.
func MarkAutoTrainStarted(dataDir, domain, model string, exampleCount int) error {
	return markAutoTrain(dataDir, domain, model, exampleCount, "started")
}

// MarkAutoTrainCompleted records a successful auto-train for domain.
func MarkAutoTrainCompleted(dataDir, domain, model string, exampleCount int) error {
	return markAutoTrain(dataDir, domain, model, exampleCount, "completed")
}

// MarkAutoTrainFailed records a failed auto-train so a later pass can retry.
func MarkAutoTrainFailed(dataDir, domain, model string, exampleCount int) error {
	return markAutoTrain(dataDir, domain, model, exampleCount, "failed")
}

func markAutoTrain(dataDir, domain, model string, exampleCount int, status string) error {
	meta, err := LoadMetadata(dataDir)
	if err != nil {
		return err
	}
	if meta.LastAutoTrain == nil {
		meta.LastAutoTrain = map[string]AutoTrainRecord{}
	}
	meta.LastAutoTrain[domain] = AutoTrainRecord{
		ExampleCount: exampleCount,
		TrainedAt:    time.Now().UTC(),
		Model:        model,
		Status:       status,
	}
	return SaveMetadata(dataDir, meta)
}

// PendingAutoTrain is a durable queue entry for daemon-side auto-train.
type PendingAutoTrain struct {
	Domain       string    `json:"domain"`
	Model        string    `json:"model"`
	ExampleCount int       `json:"example_count"`
	EnqueuedAt   time.Time `json:"enqueued_at"`
}

func pendingAutoTrainPath(dataDir string) string {
	return filepath.Join(dataDir, "pending_auto_train.jsonl")
}

// EnqueueAutoTrain appends a pending train request. Deduplicates when the same
// domain+model is already pending with equal or higher example count.
func EnqueueAutoTrain(dataDir string, pending PendingAutoTrain) error {
	if dataDir == "" || pending.Domain == "" {
		return fmt.Errorf("learning: enqueue requires dataDir and domain")
	}
	if pending.EnqueuedAt.IsZero() {
		pending.EnqueuedAt = time.Now().UTC()
	}

	if existing, err := ListPendingAutoTrain(dataDir); err == nil {
		for _, p := range existing {
			if p.Domain == pending.Domain && p.Model == pending.Model && p.ExampleCount >= pending.ExampleCount {
				return nil
			}
		}
	}

	data, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(pendingAutoTrainPath(dataDir), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("learning: open pending auto train: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// ListPendingAutoTrain returns pending auto-train entries (latest per domain+model).
func ListPendingAutoTrain(dataDir string) ([]PendingAutoTrain, error) {
	path := pendingAutoTrainPath(dataDir)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	type key struct{ d, m string }
	latest := map[key]PendingAutoTrain{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var p PendingAutoTrain
		if err := json.Unmarshal(line, &p); err != nil {
			continue
		}
		if p.Domain == "" {
			continue
		}
		latest[key{p.Domain, p.Model}] = p
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	out := make([]PendingAutoTrain, 0, len(latest))
	for _, p := range latest {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		return out[i].Model < out[j].Model
	})
	return out, nil
}

// ClearPendingAutoTrain removes domain+model from the pending queue.
func ClearPendingAutoTrain(dataDir, domain, model string) error {
	path := pendingAutoTrainPath(dataDir)
	entries, err := ListPendingAutoTrain(dataDir)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		if os.IsNotExist(err) && len(entries) == 0 {
			return nil
		}
		return err
	}
	w := bufio.NewWriter(out)
	for _, p := range entries {
		if p.Domain == domain && p.Model == model {
			continue
		}
		data, mErr := json.Marshal(p)
		if mErr != nil {
			out.Close()
			os.Remove(tmp)
			return mErr
		}
		w.Write(data)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
