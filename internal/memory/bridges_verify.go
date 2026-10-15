package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// ErrBridgeVerify wraps every verifier transport failure so callers can
// distinguish "LLM unavailable" from per-bridge drops.
var ErrBridgeVerify = errors.New("bridge verification: llm call failed")

// errBridgeVerdictMalformed wraps unparseable verdict output (fail-closed:
// the bridge is dropped, not kept).
var errBridgeVerdictMalformed = errors.New("bridge verification: malformed verdict")

// BridgeVerifier decides whether one bridging fact is entailed by its source
// memories. Pluggable for tests, mirroring the BridgeGenerator pattern.
type BridgeVerifier interface {
	// VerifyBridge returns (supported, reason, err) for one bridge record.
	// supported=true marks the bridge entailed by its evidence.
	VerifyBridge(ctx context.Context, rec BridgeRecord) (bool, string, error)
}

// bridgeVerdict is the grammar-constrained verdict shape the local model
// must emit: {"supported": bool, "reason": string} (branch 09 contract 3).
type bridgeVerdict struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason"`
}

// llmBridgeVerifier is the production BridgeVerifier: it wraps the manager's
// llm.Chatter and attaches llm.BridgeVerdictGrammar() via WithRawGrammar so
// llama.cpp-style local endpoints constrain output to the decodable shape.
type llmBridgeVerifier struct {
	client llm.Chatter
}

var _ BridgeVerifier = (*llmBridgeVerifier)(nil)

// NewBridgeEvidenceVerifier builds the production entailment verifier over a
// chatter. The verdict is grammar-constrained (llm.BridgeVerdictGrammar) —
// no free-text parsing.
func NewBridgeEvidenceVerifier(client llm.Chatter) BridgeVerifier {
	return &llmBridgeVerifier{client: client}
}

// VerifyBridge assembles the capped entailment prompt (bridge fact + its
// evidence memories, capped at MaxBridgeSourceMemories sources) and calls the
// chatter with the verdict grammar attached.
func (v *llmBridgeVerifier) VerifyBridge(ctx context.Context, rec BridgeRecord) (bool, string, error) {
	var sb strings.Builder
	sb.WriteString("You verify bridging facts. Given a candidate bridging fact and the memory excerpts it was derived from, decide whether the fact is actually ENTAILED by those excerpts — true information about the shared entity that follows from them, not a hallucination. Bridging facts may combine information ACROSS the excerpts, but every claim must be grounded in at least one of them.\n")
	sb.WriteString("Respond with ONLY a JSON object: {\"supported\": true or false, \"reason\": \"short explanation\"}.\n\n")
	sb.WriteString(fmt.Sprintf("Bridging fact: %s\n\nEvidence memories:\n", rec.Content))
	for i, ev := range rec.EvidenceIDs {
		content := bridgeEvidenceContent(rec, ev)
		sb.WriteString(fmt.Sprintf("- [%s] %s\n", ev, content))
		i++
		if i >= MaxBridgeSourceMemories {
			break
		}
	}

	msgs := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: "You verify bridging facts against their source memories as constrained JSON."},
		{Role: llm.RoleUser, Content: sb.String()},
	}
	resp, err := v.client.Chat(ctx, msgs, llm.WithRawGrammar(llm.BridgeVerdictGrammar()))
	if err != nil {
		return false, "", err
	}
	var vd bridgeVerdict
	if err := json.Unmarshal([]byte(resp.Content), &vd); err != nil {
		return false, "", fmt.Errorf("%w: %w", errBridgeVerdictMalformed, err)
	}
	return vd.Supported, vd.Reason, nil
}

// bridgeEvidenceContent returns the evidence memory's content when the
// generator embedded it in metadata, else empty — the IDs alone still let a
// local model judge entailment from what it can see; empty evidence content
// is treated as unverifiable by the prompt.
func bridgeEvidenceContent(rec BridgeRecord, evidenceID string) string {
	if rec.Metadata == nil {
		return ""
	}
	key := BridgeMetaEvidenceContentPrefix + evidenceID
	if s, ok := rec.Metadata[key].(string); ok {
		return s
	}
	return ""
}

// bridgeHasEvidenceContent reports whether ANY evidence_content_<id> key is
// present in the record's metadata. A record with none cannot be judged: the
// verifier's prompt shows empty excerpts, so its verdict measures the missing
// inputs, not the fact (bughunt 2026-10-14 — verify=true deleted every
// pre-fix bridge on that basis).
func bridgeHasEvidenceContent(rec BridgeRecord) bool {
	if rec.Metadata == nil {
		return false
	}
	for _, ev := range rec.EvidenceIDs {
		if s, ok := rec.Metadata[BridgeMetaEvidenceContentPrefix+ev].(string); ok && s != "" {
			return true
		}
	}
	return false
}

// VerifyBridges runs the entailment-verification pass over every persisted
// bridge (branch 09 leaf B03):
//
//   - cfg.Verify false (or a nil store/verifier): no-op — zero verifier
//     calls, every bridge stays exactly as persisted (verified="false").
//     Verify is a quality gate, NOT a recall gate; recall-side weighting of
//     unverified bridges is leaf B04's k_b concern.
//   - cfg.Verify true: one verifier call per bridge (v1; batching is a
//     follow-up). Unsupported verdicts DELETE the bridge before it can ever
//     be retrievable; survivors get verified=true re-persisted.
//
// Returns (kept, dropped) counts. A verifier transport error aborts the pass
// with an error wrapping ErrBridgeVerify; bridges already processed keep
// their state (best-effort partial progress is preserved).
func VerifyBridges(ctx context.Context, store *BridgeStore, verifier BridgeVerifier, cfg BridgesConfig) (kept int, dropped int, err error) {
	if store == nil || verifier == nil || !cfg.Verify {
		// verify=false is a quality-gate off switch: zero verifier calls,
		// bridges persist untouched. kept still reports the pool size so
		// callers can log "kept N unverified".
		if store != nil && !cfg.Verify {
			if n, cerr := store.Count(ctx); cerr == nil {
				return n, 0, nil
			}
		}
		return 0, 0, nil
	}
	return verifyBridgesListed(ctx, store, verifier, nil)
}

// verifyBridgesListed verifies the given bridge list, or — when bridges is
// nil — every persisted bridge (side-table scan). Split from VerifyBridges
// so the generation-flow caller can pass exactly the bridges it just wrote.
func verifyBridgesListed(ctx context.Context, store *BridgeStore, verifier BridgeVerifier, bridges []BridgeRecord) (int, int, error) {
	if bridges == nil {
		var err error
		bridges, err = store.listAllBridges(ctx)
		if err != nil {
			return 0, 0, fmt.Errorf("bridge verification: list: %w", err)
		}
	}
	kept, dropped := 0, 0
	for _, rec := range bridges {
		// Can this bridge be judged at all? A record with NO
		// evidence_content_<id> metadata shows the verifier empty
		// excerpts, so any negative verdict measures the missing prompt
		// inputs, not the fact. Skipping the judgeable check first is
		// what keeps verify=true (the default) from destroying every
		// pre-fix bridge (bughunt 2026-10-14): bridges persisted before
		// generation embedded evidence content are retained unverified
		// instead of deleted on "unsupported".
		judgeable := bridgeHasEvidenceContent(rec)
		supported, _, verr := verifier.VerifyBridge(ctx, rec)
		if verr != nil {
			// Malformed verdict output is a per-bridge failure, not a
			// transport failure: fail CLOSED (drop the bridge) rather
			// than leaving an unverifiable fact permanently retrievable
			// — but only when the verifier actually SAW the sources.
			if errors.Is(verr, errBridgeVerdictMalformed) {
				if !judgeable {
					kept++ // retained, stays verified="false"
					continue
				}
				if derr := store.DeleteBridge(ctx, rec.ID); derr != nil {
					return kept, dropped, fmt.Errorf("bridge verification: delete %s: %w", rec.ID, derr)
				}
				dropped++
				continue
			}
			// Transport-class failure: abort the pass; already-processed
			// bridges keep their state.
			return kept, dropped, fmt.Errorf("%w: bridge %s: %w", ErrBridgeVerify, rec.ID, verr)
		}
		if !supported {
			if !judgeable {
				// Same blind-record rule for an explicit unsupported
				// verdict: not evidence of anything about the fact.
				kept++
				continue
			}
			// Unsupported: DELETE before it is ever retrievable.
			if derr := store.DeleteBridge(ctx, rec.ID); derr != nil {
				return kept, dropped, fmt.Errorf("bridge verification: delete %s: %w", rec.ID, derr)
			}
			dropped++
			continue
		}
		// Survivor: verified=true (metadata row only — the vector row is
		// untouched, so no embedding round-trip is needed).
		if perr := store.MarkVerified(ctx, rec.ID); perr != nil {
			return kept, dropped, fmt.Errorf("bridge verification: mark verified %s: %w", rec.ID, perr)
		}
		kept++
	}
	return kept, dropped, nil
}

// invalidateBridgesForMemory is the shared cascade-invalidation helper
// (branch 09 contract 4): deletes every bridge whose evidence_ids contain
// memoryID. Called from all three source-memory mutation seams — Manager.Delete
// (delete), StoreVersioned/MarkSuperseded (update — the old version is no
// longer a current fact), and consolidation expiry. Idempotent: a second call
// finds no bridges and deletes nothing. Returns an error so callers can log
// it; provenance cleanup must never fail the source mutation itself.
func invalidateBridgesForMemory(ctx context.Context, store *BridgeStore, memoryID string) error {
	if store == nil || strings.TrimSpace(memoryID) == "" {
		return nil
	}
	bridges, err := store.ListBridgesByEvidence(ctx, memoryID)
	if err != nil {
		return fmt.Errorf("bridge invalidation: evidence scan for %s: %w", memoryID, err)
	}
	for _, rec := range bridges {
		if err := store.DeleteBridge(ctx, rec.ID); err != nil {
			return fmt.Errorf("bridge invalidation: delete %s (evidence %s): %w", rec.ID, memoryID, err)
		}
	}
	return nil
}

// compile-time interface checks
var (
	_ BridgeVerifier = (*llmBridgeVerifier)(nil)
	_ slog.Handler   = slog.DiscardHandler
)
