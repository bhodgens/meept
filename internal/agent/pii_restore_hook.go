package agent

import (
	"context"
	"log/slog"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	intsecurity "github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/internal/security/pii"
)

// PiiRestoreHook restores swap tokens in model output before any
// persistence surface. Two lanes, both session-bounded (research findings
// D4/D5, L7):
//
//  1. PII tokens: [PII:<uuid>] placeholders minted by the pii.Swapper in
//     this session resolve back to their original values via the session-
//     scoped SwapStore. Tokens minted in ANOTHER session never resolve
//     here - the store refuses cross-session lookups (cross-session
//     restore is an info leak, not a convenience).
//  2. Secret placeholders: #XXXX# placeholders minted by the process-wide
//     SecretObfuscator pass restore via Deobfuscate. The obfuscator's maps
//     are NOT session-scoped (they are process-wide by design - the
//     pre-LLM obfuscation hook obfuscated this session's context in this
//     process), so this lane is gated on cfg.SecretRestore (research D6:
//     learning-capture redaction shares the gate) and on the swapper AND
//     obfuscator being present.
//
// One-to-one value-keyed restore only (L7 LiteLLM wrong-restore
// corruption class): placeholders resolve through exact fixed-string
// replacement against the maps that minted them; no partial or positional
// rewriting.
//
// Value discipline: raw values and tokens never reach logs - logging is
// counts and session identifiers only (leakage class C3).
type PiiRestoreHook struct {
	swapper    *pii.Swapper
	obfuscator *intsecurity.SecretObfuscator
	cfg        config.PiiConfig
	logger     *slog.Logger
}

// NewPiiRestoreHook builds the output restore hook. Typed-nil guards: a
// nil swapper (the mandatory lane) or a nil obfuscator (needed by the
// secret lane whenever the feature is on) yields a nil hook so callers
// register nothing - the disabled path stays byte-identical.
func NewPiiRestoreHook(swapper *pii.Swapper, obfuscator *intsecurity.SecretObfuscator, cfg config.PiiConfig, logger *slog.Logger) *PiiRestoreHook {
	if swapper == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PiiRestoreHook{
		swapper:    swapper,
		obfuscator: obfuscator,
		cfg:        cfg,
		logger:     logger.With("component", "pii-restore"),
	}
}

// Enabled reports whether the feature flag is on (the store may exist
// while the lane is off; RestoreOutput must still refuse to mutate).
func (h *PiiRestoreHook) Enabled() bool { return h != nil && h.cfg.Enabled }

// RestoreOutput restores session tokens and (when gated) secret
// placeholders in text. A disabled hook returns the input byte-identical;
// an empty session restore refuses to run the secret lane either (no
// session context = no provenance).
func (h *PiiRestoreHook) RestoreOutput(text, sessionID string) string {
	if h == nil || !h.cfg.Enabled {
		return text
	}

	restored := text
	piiCount := 0
	if sessionID != "" {
		var n int
		restored, n = h.swapper.Restore(restored, sessionID)
		piiCount = n
	}

	secretCount := 0
	if h.cfg.SecretRestore && h.obfuscator != nil {
		deobfuscated := h.obfuscator.Deobfuscate(restored)
		if deobfuscated != restored {
			// Count one event, never the values: the obfuscator's
			// per-placeholder detail stays in-process.
			secretCount = countPlaceholders(restored) - countPlaceholders(deobfuscated)
			if secretCount < 0 {
				secretCount = 0
			}
			restored = deobfuscated
		}
	}

	if piiCount > 0 || secretCount > 0 {
		h.logger.Debug("restored swap tokens in output",
			"session_id", sessionID,
			"pii_tokens", piiCount,
			"secret_placeholders", secretCount,
		)
	}
	return restored
}

// countPlaceholders counts #XXXX#-shaped placeholder occurrences for the
// count-only log line. Cheap fixed-shape scan; never inspects values.
func countPlaceholders(text string) int {
	count := 0
	for i := 0; i+1 < len(text); i++ {
		if text[i] != '#' {
			continue
		}
		j := i + 1
		for j < len(text) && text[j] != '#' {
			j++
		}
		if j < len(text) && j > i+1 {
			count++
			i = j
		}
	}
	return count
}

// TransformContext satisfies the TransformContextHook seam so the hook CAN
// be registered in the registry (C5-B order pin). It is intentionally a
// pass-through: restore is OUTPUT-side; the context must keep its masked
// tokens so the model never sees raw values. Registration exists so the
// ordering test and components.go wiring have a real hook to pin.
func (h *PiiRestoreHook) TransformContext(_ context.Context, messages []llm.ChatMessage, toolDefs []llm.ToolDefinition) ContextTransform {
	return ContextTransform{}
}
