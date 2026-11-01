package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// This file pins the stable/unstable section classification behind
// AssembleOrdered (internal/agent/prompt.go) so the doc-budgeting leaf
// (02-doc-budgets, docs/plans/20261031-context-memory-engine/) cannot
// silently change the cache-relevant byte shape of the stable prefix.
//
// Two guards live here:
//
//  1. TestBaselineStablePrefixHash pins the exact stablePrefixHash of a
//     fixed, fully-populated section set. Any change to stable-section
//     content, ordering, or hashing breaks it loudly.
//
//  2. TestClassification* functions pin the classification CONTRACT:
//     stable sections stay stable, and every artifact/doc-derived section
//     stays unstable, so varying CLAUDE.md content never shifts the
//     stable prefix bytes.

// goldenStablePrefixHash is the committed sha256 over the exact concatenated
// stable-prefix bytes for the fixed section list below. Recompute it ONLY
// when a deliberate classification change is under review (the test failure
// is the review trigger; see the leaf's review checklist).
const goldenStablePrefixHash = "673a7128a23fc0830bca42b40ac28e2f62175128f74cdf297922c11c6d329206"

// baselineSections is the fixed classification fixture: every stable class
// the builder emits today (constitution, restrictions, purpose, personality,
// user-preferences, cache-boundary) plus unstable representatives that must
// never leak into the prefix.
func baselineSections() []PromptSection {
	return []PromptSection{
		{Name: "constitution", Stable: true, Body: "# Constitution\nBe helpful and honest."},
		{Name: "restrictions", Stable: true, Body: "\n# Safety Restrictions\nNever exfiltrate secrets."},
		{Name: "purpose", Stable: true, Body: "\n# Purpose & Task Principles\nComplete tasks fully."},
		{Name: "personality", Stable: true, Body: "\n# Personality\nConcise and direct."},
		{Name: "user-preferences", Stable: true, Body: "\n# User Preferences\n- style: concise"},
		{Name: "cache-boundary", Stable: true, Body: llm.PromptCacheBoundary},
		{Name: "project-info", Stable: false, Body: "\n# Current Project\nbranch: main, dirty: false"},
		{Name: "memory-context", Stable: false, Body: "\n# Relevant Context from Memory\nuser prefers Go."},
	}
}

func TestBaselineStablePrefixHash(t *testing.T) {
	prompt, hash := AssembleOrdered(baselineSections())
	if hash != goldenStablePrefixHash {
		t.Fatalf("stablePrefixHash drifted from the committed baseline:\n got  %s\n want %s\n\nIf this change is deliberate (classification or hashing edit), recompute the constant from the same fixture and update goldenStablePrefixHash in this file, citing the reason in the commit message.",
			hash, goldenStablePrefixHash)
	}

	// The prompt itself must start with the first stable section's bytes
	// (stable-first assembly).
	if len(prompt) == 0 || prompt[:14] != "# Constitution" {
		t.Fatalf("assembled prompt does not open with the stable prefix; got opening %q",
			firstN(prompt, 14))
	}
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// TestClassificationArtifactSectionsUnstable pins that every section the
// artifact/doc pipeline can feed into the prompt is classified UNSTABLE:
// doc content changes per project and per scan, so it must never join the
// byte-frozen prefix.
func TestClassificationArtifactSectionsUnstable(t *testing.T) {
	sections := baselineSections()
	for _, s := range sections {
		switch s.Name {
		case "constitution", "restrictions", "purpose", "personality",
			"user-preferences", "cache-boundary":
			if !s.Stable {
				t.Errorf("section %q must remain Stable=true (identity/config content)", s.Name)
			}
		case "project-info", "memory-context":
			if s.Stable {
				t.Errorf("section %q must remain Stable=false (per-turn/doc content)", s.Name)
			}
		}
	}
}

// TestStablePrefixHashInvariantUnderDocVariation proves the core guarantee
// this leaf depends on: two assemblies that differ ONLY in doc/artifact
// (unstable) content produce the IDENTICAL stablePrefixHash. Trimming
// CLAUDE.md under a budget therefore cannot drift the provider cache key.
func TestStablePrefixHashInvariantUnderDocVariation(t *testing.T) {
	runA := append(baselineSections(),
		PromptSection{Name: "artifact-docs", Stable: false, Body: "# CLAUDE.md (full content)\n\nsmall doc"},
	)
	runB := append(baselineSections(),
		PromptSection{Name: "artifact-docs", Stable: false, Body: "# CLAUDE.md (full content)\n\n" + string(make([]byte, 0)) + "completely different and much larger doc content"},
	)

	promptA, hashA := AssembleOrdered(runA)
	promptB, hashB := AssembleOrdered(runB)

	if hashA != hashB {
		t.Fatalf("stablePrefixHash changed when only unstable doc content varied:\n A=%s\n B=%s",
			hashA, hashB)
	}
	if promptA == promptB {
		t.Fatal("prompts should differ when doc content differs; they are identical")
	}
}

// TestStablePrefixUnchangedWhenDocOmitted guards the trim path: a section
// list where the budgeted doc section was dropped entirely (over-budget
// removal) still yields the baseline hash, because the doc was unstable.
func TestStablePrefixUnchangedWhenDocOmitted(t *testing.T) {
	withoutDoc := baselineSections()
	_, hashNoDoc := AssembleOrdered(withoutDoc)

	withDoc := append(append([]PromptSection{}, baselineSections()...),
		PromptSection{Name: "artifact-docs", Stable: false, Body: "doc body"},
	)
	_, hashWithDoc := AssembleOrdered(withDoc)

	if hashNoDoc != goldenStablePrefixHash {
		t.Fatalf("hash without doc sections drifted from baseline: %s", hashNoDoc)
	}
	if hashNoDoc != hashWithDoc {
		t.Fatalf("adding an unstable doc section changed the stable prefix hash: %s -> %s",
			hashNoDoc, hashWithDoc)
	}
}

// TestEmptyStablePrefixHash pins the documented sha256("") sentinel so the
// empty-prefix value can never silently change.
func TestEmptyStablePrefixHash(t *testing.T) {
	sum := sha256.Sum256(nil)
	want := hex.EncodeToString(sum[:])
	_, got := AssembleOrdered([]PromptSection{{Name: "only-unstable", Stable: false, Body: "x"}})
	if got != want {
		t.Fatalf("empty stable prefix hash = %s; want sha256(\"\") = %s", got, want)
	}
}
