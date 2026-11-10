package daemon

// ledger_wiring.go — branch 03-voice impl leaf 03-voice/01: daemon-owned
// construction of the employee ledger store, handed to the voice release
// hook (per-call brief), the tool surface, and the ops leaves.
//
// Degrade posture (PINNED — leaf 04-01 relies on it): buildLedger opens
// the store at <DataDir>/ledger/ledger.db; ANY open failure logs WARN and
// returns nil. A nil store means the feature is simply absent — voice
// calls, the daemon boot, and every consumer run unchanged (the brief
// no-ops via the nil ledgerStore guard; 04-01 must never register its
// integrity job against a nil store). No config knob in v1: the ledger
// is on iff the store opened.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/ledger"
	"github.com/caimlas/meept/internal/llm"
)

// buildLedger constructs the single employee-ledger Store for the daemon.
//
// Responsibilities (03-voice/01 task 1):
//   - open <cfg.Daemon.DataDir>/ledger/ledger.db (mkdir -p the dir first);
//     failure -> WARN + nil (never fatal — see the pinned degrade posture).
//   - compose the after-append hook: the S4 publish hook
//     (buildLedgerAfterAppendHook, leaf 02-surfaces/03) is installed now;
//     leaf 04-02 extends the SAME composition with the audit-chain row
//     over the `chain` handle (nil chain -> audit skip, per 04-02 task 1).
//
// The ToolRegistry is not reachable from this call site (it lives on
// Components after construction), so the store handle is returned for the
// site that HAS c.ToolRegistry to pass to builtin.RegisterLedgerTools —
// the leaf chooses location; tests gate behavior, not location.
func buildLedger(cfg *config.Config, msgBus *bus.MessageBus, chain *auditlog.Store, logger *slog.Logger) *ledger.Store {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg == nil || cfg.Daemon.DataDir == "" {
		logger.Warn("ledger wiring: no daemon data dir configured; ledger disabled")
		return nil
	}

	dir := filepath.Join(cfg.Daemon.DataDir, "ledger")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logger.Warn("ledger wiring: mkdir store dir failed; ledger disabled",
			"path", dir, "error", err)
		return nil
	}

	store, err := ledger.Open(filepath.Join(dir, "ledger.db"), logger.With("component", "ledger-store"))
	if err != nil {
		// Pinned degrade posture: WARN + nil, voice + daemon unaffected.
		logger.Warn("ledger wiring: open ledger store failed; ledger disabled",
			"path", filepath.Join(dir, "ledger.db"), "error", err)
		return nil
	}

	// After-append hook composition (post-commit, Store-internal):
	// publish first (S4), audit chained next by leaf 04-02 in this same
	// composition point.
	store.WithAfterAppend(buildLedgerAfterAppendHook(msgBus, logger.With("component", "ledger-publish")))

	// `chain` is intentionally unused until leaf 04-02 folds the
	// audit-chain hook into the composition above (nil chain -> skip).
	_ = chain

	logger.Info("ledger wiring: employee ledger store ready", "path", filepath.Join(dir, "ledger.db"))
	return store
}

// ledgerAuditChain resolves the shared audit-chain handle for the ledger
// hook composition, over the same DB pool the voice dial audit rows use
// (components.go voice-audit precedent: OpenStoreFromDB's Close is a
// no-op on caller-owned handles). Fail-soft: nil chain -> the 04-02
// audit hook skips silently.
func ledgerAuditChain(c *Components, logger *slog.Logger) *auditlog.Store {
	chainDB := activityChainDB(c)
	if chainDB == nil {
		return nil
	}
	if s, err := auditlog.OpenStoreFromDB(chainDB, logger.With("component", "ledger-audit")); err == nil {
		return s
	} else {
		logger.Warn("ledger wiring: audit chain store unavailable; ledger audit rows degraded", "error", err)
		return nil
	}
}

// ledgerBriefLLMFunc adapts the daemon's EXISTING llm client onto the
// brief seam's plain func form func(prompt string) (string, error) —
// no new HTTP client is constructed here (the leaf pins that; the caller
// passes c.LLMClient or an employee-pinned client resolved through the
// existing resolution path). Request posture is frozen: max_tokens 500,
// temperature 0.2, no tools, single user message (the frozen system
// lines ride inline in the prompt built by buildVoiceBriefPrompt).
func ledgerBriefLLMFunc(client *llm.Client) func(prompt string) (string, error) {
	if client == nil {
		return nil
	}
	return func(prompt string) (string, error) {
		resp, err := client.Chat(context.Background(), []llm.ChatMessage{
			{Role: llm.RoleUser, Content: prompt},
		}, llm.WithMaxTokens(voiceBriefMaxTokens), llm.WithTemperature(voiceBriefTemp))
		if err != nil {
			return "", fmt.Errorf("voice brief llm: %w", err)
		}
		if resp == nil {
			return "", fmt.Errorf("voice brief llm: nil response")
		}
		return resp.Content, nil
	}
}
