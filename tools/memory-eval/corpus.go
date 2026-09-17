package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// goldSet holds the gold labels for one corpus segment.
type goldSet struct {
	Claims      []string `json:"claims"`
	Decisions   []string `json:"decisions"`
	Predictions []string `json:"predictions"`
	Lessons     []string `json:"lessons"` // distill gold (principles)
	Decoys      int      `json:"decoys"`
}

type segment struct {
	ID       string   `json:"id"`
	Messages []string `json:"messages"`
	Gold     goldSet  `json:"gold"`
	// Category documents the segment's intent (decoy-heavy, multi-claim,
	// prediction, decision, contradiction, dedupe-pair, baseline). Free text.
	Category string   `json:"category,omitempty"`
	PairWith string   `json:"pair_with,omitempty"`
}

type corpus struct {
	Segments []segment `json:"segments"`
}

func loadCorpus(path string) (*corpus, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var c corpus
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if len(c.Segments) == 0 {
		return nil, fmt.Errorf("corpus has no segments")
	}
	return &c, nil
}

func validateCorpus(c *corpus) error {
	ids := map[string]bool{}
	for i, s := range c.Segments {
		if s.ID == "" {
			return fmt.Errorf("segment %d: missing id", i)
		}
		if ids[s.ID] {
			return fmt.Errorf("segment %d: duplicate id %q", i, s.ID)
		}
		ids[s.ID] = true
		if len(s.Messages) == 0 {
			return fmt.Errorf("segment %s: no messages", s.ID)
		}
		g := s.Gold
		if len(g.Claims) == 0 && len(g.Decisions) == 0 && len(g.Predictions) == 0 &&
			len(g.Lessons) == 0 && g.Decoys == 0 {
			return fmt.Errorf("segment %s: empty gold", s.ID)
		}
		if g.Decoys < 0 {
			return fmt.Errorf("segment %s: negative decoys", s.ID)
		}
	}
	return nil
}

func printCorpusSummary(c *corpus) {
	claims, decisions, predictions, decoys, lessons, paired := 0, 0, 0, 0, 0, 0
	cats := map[string]int{}
	for _, s := range c.Segments {
		claims += len(s.Gold.Claims)
		decisions += len(s.Gold.Decisions)
		predictions += len(s.Gold.Predictions)
		decoys += s.Gold.Decoys
		lessons += len(s.Gold.Lessons)
		if s.PairWith != "" {
			paired++
		}
		cat := s.Category
		if cat == "" {
			cat = "unlabeled"
		}
		cats[cat]++
	}
	fmt.Printf("Corpus: %d segments | gold: %d claims, %d decisions, %d predictions, %d lessons, %d decoys | paired: %d\n",
		len(c.Segments), claims, decisions, predictions, lessons, decoys, paired)
	keys := make([]string, 0, len(cats))
	for k := range cats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %-16s %d\n", k+":", cats[k])
	}
}
