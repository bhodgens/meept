package learning

import (
	"log/slog"
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Valid feedback labels accepted by ApplyUserFeedback / ScoreExample.
const (
	FeedbackPositive = "positive"
	FeedbackNegative = "negative"
	FeedbackNeutral  = "neutral"
)

// NormalizeFeedback maps user input to a canonical feedback label.
func NormalizeFeedback(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "positive", "pos", "+1", "up", "good":
		return FeedbackPositive, true
	case "negative", "neg", "-1", "down", "bad":
		return FeedbackNegative, true
	case "neutral", "none", "clear", "reset":
		return FeedbackNeutral, true
	default:
		return "", false
	}
}

// FeedbackResult summarizes how many raw-capture rows were updated.
type FeedbackResult struct {
	Matched int `json:"matched"`
	Updated int `json:"updated"`
}

// ApplyUserFeedback updates TaskOutcome.UserFeedback on trajectories in
// raw_captures.jsonl that match sessionID (and optional trajectoryID).
// Neutral clears feedback. Rewrite is atomic (temp + rename). Also re-scores
// matching domain dataset rows so feedback is not stuck behind dedup.
func ApplyUserFeedback(dataDir, sessionID, trajectoryID, feedback string) (*FeedbackResult, error) {
	if strings.TrimSpace(dataDir) == "" {
		return nil, fmt.Errorf("learning: dataDir must not be empty")
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("learning: sessionID must not be empty")
	}
	label, ok := NormalizeFeedback(feedback)
	if !ok {
		return nil, fmt.Errorf("learning: invalid feedback %q (want positive|negative|neutral)", feedback)
	}
	stored := label
	if label == FeedbackNeutral {
		stored = ""
	}

	rawPath := filepath.Join(dataDir, "raw_captures.jsonl")
	f, err := os.Open(rawPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &FeedbackResult{}, nil
		}
		return nil, fmt.Errorf("learning: open raw captures: %w", err)
	}
	defer f.Close()

	tmpPath := rawPath + ".feedback.tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("learning: create temp: %w", err)
	}
	w := bufio.NewWriter(out)

	result := &FeedbackResult{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var traj ResearchTrajectory
		if err := json.Unmarshal(line, &traj); err != nil {
			w.Write(line)
			w.WriteByte('\n')
			continue
		}
		match := traj.SessionID == sessionID
		if match && trajectoryID != "" {
			match = traj.ID == trajectoryID
		}
		if match {
			result.Matched++
			if traj.TaskOutcome.UserFeedback != stored {
				traj.TaskOutcome.UserFeedback = stored
				result.Updated++
			}
			data, mErr := json.Marshal(traj)
			if mErr != nil {
				out.Close()
				os.Remove(tmpPath)
				return nil, fmt.Errorf("learning: marshal updated trajectory: %w", mErr)
			}
			w.Write(data)
			w.WriteByte('\n')
			continue
		}
		w.Write(line)
		w.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("learning: scan raw captures: %w", err)
	}
	if err := w.Flush(); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return nil, err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return nil, err
	}
	if err := os.Rename(tmpPath, rawPath); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("learning: rename feedback rewrite: %w", err)
	}

	if result.Updated > 0 {
		if err := rescoreDatasetsForSession(dataDir, sessionID, trajectoryID); err != nil {
			slog.Warn("learning: rescore datasets failed", "error", err)
		}
	}
	return result, nil
}

func rescoreDatasetsForSession(dataDir, sessionID, trajectoryID string) error {
	rawPath := filepath.Join(dataDir, "raw_captures.jsonl")
	f, err := os.Open(rawPath)
	if err != nil {
		return err
	}
	defer f.Close()

	type scored struct {
		domain      string
		instruction string
		score       float64
	}
	byKey := map[string]scored{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var traj ResearchTrajectory
		if err := json.Unmarshal(sc.Bytes(), &traj); err != nil {
			continue
		}
		if traj.SessionID != sessionID {
			continue
		}
		if trajectoryID != "" && traj.ID != trajectoryID {
			continue
		}
		if strings.TrimSpace(traj.Synthesis) == "" || traj.Domain == "" {
			continue
		}
		instruction := strings.TrimSpace(traj.Intent)
		if instruction == "" {
			instruction = traj.Query
		}
		if instruction == "" {
			continue
		}
		score := ScoreExample(traj)
		key := traj.Domain + "\x00" + instruction
		byKey[key] = scored{domain: traj.Domain, instruction: instruction, score: score}
	}
	if err := sc.Err(); err != nil {
		return err
	}

	datasetsDir := filepath.Join(dataDir, "datasets")
	for _, s := range byKey {
		if err := updateDomainExampleScore(datasetsDir, s.domain, sessionID, s.instruction, s.score); err != nil {
			slog.Warn("learning: update domain example score failed", "domain", s.domain, "error", err)
		}
	}
	return nil
}

func updateDomainExampleScore(datasetsDir, domain, sessionID, instruction string, score float64) error {
	path := filepath.Join(datasetsDir, domain+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(out)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	changed := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ex TrainingExample
		if err := json.Unmarshal(line, &ex); err != nil {
			w.Write(line)
			w.WriteByte('\n')
			continue
		}
		if ex.Metadata.SessionID == sessionID && ex.Instruction == instruction {
			ex.Metadata.QualityScore = score
			data, mErr := json.Marshal(ex)
			if mErr != nil {
				w.Write(line)
				w.WriteByte('\n')
				continue
			}
			w.Write(data)
			w.WriteByte('\n')
			changed = true
			continue
		}
		w.Write(line)
		w.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
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
	if !changed {
		os.Remove(tmp)
		return nil
	}
	return os.Rename(tmp, path)
}
