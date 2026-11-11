package agent

import (
	"context"
	"log/slog"
	"strings"

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
//  2. Secret placeholders: #XXXXXXXXXXXX# placeholders minted by the
//     process-wide SecretObfuscator pass restore via session-scoped
//     RestoreOutput (falling back to the legacy sessionless Deobfuscate
//     for untagged mints). The obfuscator's value maps are process-wide
//     (the pre-LLM obfuscation hook obfuscated this session's context in
//     this process), but each placeholder is TAGGED with its minting
//     session: RestoreOutput only resolves that session's entries
//     (wiring-gap leaf 07 T2). This lane is gated on cfg.SecretRestore
//     (research D6: learning-capture redaction shares the gate) and on
//     the swapper AND obfuscator being present.
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
		// Session-scoped restore (wiring-gap leaf 07 T2): placeholders
		// minted in OTHER sessions never resolve here. The fallback lane
		// resolves ONLY untagged (pre-deploy) placeholders — a scoped
		// restore that made no progress must not re-open the cross-session
		// leak the scoped lane exists to refuse (bughunt 2026-11-06
		// security F-3; the old full-Deobfuscate fallback fired exactly
		// when text carried another session's tagged placeholder).
		deobfuscated := h.obfuscator.RestoreOutput(restored, sessionID)
		if untagged := h.obfuscator.DeobfuscateUntagged(deobfuscated); untagged != deobfuscated {
			deobfuscated = untagged
		}
		if deobfuscated != restored {
			// Count one event, never the values: the obfuscator's
			// per-placeholder detail stays in-process.
			secretCount = max(countPlaceholders(restored)-countPlaceholders(deobfuscated), 0)
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

// countPlaceholders counts #XXXXXXXXXXXX#-shaped placeholder occurrences for
// the count-only log line. Cheap fixed-shape scan; never inspects values.
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

// MaskOutboundScoped is the pre-LLM masking half of the unmasking proxy
// (agent-boundary-hardening BND-1; master contract C1-C scenario 03(a):
// provider-bound context carries only [PII:*] tokens). It replaces every
// detected PII span in message text with a session-scoped token minted
// through the SAME Swapper the restore lane resolves through, so the later
// RestoreOutput call on the response un-masks with the mapping this call
// recorded. Called by the loop on the CLONED provider-bound message slice
// (after cloneMessagesForTransform) — stored session history is never
// touched; only what crosses to the provider this iteration.
//
// Refusals (each returns the input unchanged — the disabled-path
// discipline):
//   - nil hook / disabled config: feature off, byte-identical.
//   - empty sessionID: tokens minted without a session scope can never be
//     restored (the store refuses empty-session resolution), so scope-less
//     masking is refused, not degraded.
//   - empty message slice.
//
// Tool-call arguments are NOT masked: they are data addressed at this
// session's tools, and masking them would corrupt tool semantics while the
// responses they produce flow back only into this session. Content and text
// content parts — the same surfaces the serializers emit (Parts take
// precedence over Content in every provider serializer, llm/models.go), and
// the same surface the restore lane un-masks.
//
// C18 (bughunt 2026-11-10): Parts-bearing messages (multimodal turns, resume
// replays carrying turn.Parts) previously crossed to the provider UNMASKED —
// the mask touched only Content while every serializer PREFERS Parts. Text
// parts now go through the same swapper; non-text parts (image URLs) are
// left alone; and when Parts were masked the flat Content is masked with the
// SAME session scope so both representations stay consistent (the model may
// see either wire form, and the tokens resolve through one store).
func (h *PiiRestoreHook) MaskOutboundScoped(messages []llm.ChatMessage, sessionID string) []llm.ChatMessage {
	if h == nil || !h.cfg.Enabled || sessionID == "" || len(messages) == 0 {
		return messages
	}

	total := 0
	for i := range messages {
		if messages[i].Content != "" {
			masked, n := h.swapper.Mask(messages[i].Content, sessionID)
			if n > 0 {
				messages[i].Content = masked
				total += n
			}
		}
		// C18: text-bearing parts through the same swapper/session scope.
		for j := range messages[i].Parts {
			p := &messages[i].Parts[j]
			if p.Type != "text" || p.Text == "" {
				continue
			}
			masked, n := h.swapper.Mask(p.Text, sessionID)
			if n > 0 {
				p.Text = masked
				total += n
				// Content/Parts consistency: the parts representation now
				// differs from the flat Content (the serializer prefers
				// Parts, so the wire is correct), but Content must not keep
				// raw text the parts lane just masked — mask it with the
				// same scope too.
				if messages[i].Content != "" && !strings.Contains(messages[i].Content, "[PII:") {
					if cMasked, cn := h.swapper.Mask(messages[i].Content, sessionID); cn > 0 {
						messages[i].Content = cMasked
						total += cn
					}
				}
			}
		}
	}

	if total > 0 {
		// Counts and identifiers only — raw values and tokens never reach
		// logs (leakage class C3).
		h.logger.Debug("masked PII in provider-bound context",
			"session_id", sessionID,
			"spans", total,
			"messages", len(messages),
		)
	}
	return messages
}
