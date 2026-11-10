package agent

// 00T-moment gate §3 + §4 — plan 20261110-situational leaf 01-moment/00T:
// chat-lane volatile moment section + voice-lane override append + the D1
// byte-identity golden. TESTS ONLY; RED posture expected until leaf 01/02
// lands (undefined (*AgentLoop).SetMomentRenderer).
//
// Pinned seam — implementation leaf 01/02 (02-wiring.md Task 2/3) MUST
// provide EXACTLY this lane-neutral seam on *AgentLoop:
//
//	// SetMomentRenderer installs the per-turn moment-block renderer on the
//	// lane-neutral seam. nil fn = inert (D1: pre-tree behavior, byte-for-
//	// byte identical prompts). Both lanes consume the SAME func: chat via
//	// assembleSystemPrompt volatile section, voice via
//	// buildSystemPromptWithOverride append. The daemon builds this closure
//	// over moment.Provider.Render (internal/daemon/awareness_wiring.go);
//	// the agent package must NOT import internal/moment — the seam is a
//	// plain func() string so these tests pin wiring shape, not rendering.
//	func (l *AgentLoop) SetMomentRenderer(fn func() string)
//
// Chat lane contract (master.md, StatusBar precedent at loop.go:9142):
//   - when the renderer is installed, assembleSystemPrompt adds a builder
//     custom section titled EXACTLY "moment" (02-wiring.md Task 2) whose
//     BODY is the rendered string, classified VOLATILE (stable=false) — it
//     lands after the stable prefix and NEVER participates in
//     LastStablePrefixHash.
//   - section omission rule: a renderer returning "" adds no section.
// Voice lane contract (loop.go:9125 region, voice bypass):
//   - buildSystemPromptWithOverride output == raw override + "\n\n" +
//     renderer() when a renderer is installed (override still LEADS the
//     prompt; the moment block is APPENDED after any Global Rules suffix).
//   - WITHOUT a renderer the output is byte-identical to today's behavior
//     (D1 golden, pinned as literal concatenations below — not computed by
//     calling the implementation).
//
// Identifiers are momentW*-prefixed to avoid collisions with sibling gate
// files landing in this package.

import (
	"strings"
	"testing"
)

const (
	momentWBlockV1 = "# Moment\nIt is 14:02 MST, Monday, at Rapid City shop."
	momentWBlockV2 = "# Moment\nIt is 14:33 MST, Monday, at Rapid City shop."
)

// momentWStableAnchor is a byte-invariant stable section used to prove the
// moment block rides the volatile tail (prompt-cache discipline).
func momentWStableBuilder() *PromptBuilder {
	b := NewPromptBuilder()
	b.AddSectionWithStability("Stable Anchor", "STABLE-ANCHOR-BODY", true)
	return b
}

// --- §3 chat lane ---------------------------------------------------------

// With a renderer installed the assembled prompt contains the exact block
// AFTER the stable prefix (volatile classification), and two consecutive
// turns whose renders differ (fresh time) leave LastStablePrefixHash
// untouched — the block never joins the cacheable prefix.
func TestMomentChatLaneVolatileSection(t *testing.T) {
	loop := NewAgentLoop("moment-chat-volatile", "/tmp")

	var rendered int64
	loop.SetMomentRenderer(func() string {
		rendered++
		if rendered == 1 {
			return momentWBlockV1
		}
		return momentWBlockV2
	})

	builder := momentWStableBuilder()
	first := loop.assembleSystemPrompt(builder)
	if !strings.Contains(first, momentWBlockV1) {
		t.Fatalf("turn 1 missing moment block: %q", first)
	}
	hash1 := loop.LastStablePrefixHash()
	if hash1 == "" {
		t.Fatal("expected recorded stable prefix hash after first build")
	}
	if strings.Index(first, "STABLE-ANCHOR-BODY") > strings.Index(first, momentWBlockV1) {
		t.Fatalf("moment block must follow the stable section (volatile tail):\n%q", first)
	}

	// Section identity pin (02-wiring.md Task 2): the builder gains a
	// custom section titled EXACTLY "moment" with the rendered body,
	// stability=false. (StatusBar precedent is "[status]"; moment is the
	// plain name per the wiring leaf contract.) Inspected through the
	// package-internal builder state — same package, not an API change.
	var momentSection *promptSection
	for i := range builder.customSections {
		if builder.customSections[i].title == "moment" {
			momentSection = &builder.customSections[i]
		}
	}
	if momentSection == nil {
		t.Fatalf("assembleSystemPrompt must add a custom section titled \"moment\", got sections %v", sectionTitles(builder))
	}
	if momentSection.content != momentWBlockV1 {
		t.Errorf("moment section body = %q, want exact rendered block %q", momentSection.content, momentWBlockV1)
	}
	if momentSection.stable {
		t.Error("moment section must be stability=false (volatile; prompt-cache discipline)")
	}

	second := loop.assembleSystemPrompt(momentWStableBuilder())
	if !strings.Contains(second, momentWBlockV2) {
		t.Fatalf("turn 2 missing FRESH moment block (per-turn render required):\n got: %q", second)
	}
	hash2 := loop.LastStablePrefixHash()
	if hash2 != hash1 {
		t.Fatalf("stable prefix hash drifted when only the volatile moment block changed:\n  first:  %s\n  second: %s", hash1, hash2)
	}
	if first == second {
		t.Fatal("two turns must carry fresh moment times, got identical prompts")
	}
	if rendered != 2 {
		t.Fatalf("renderer must be called once per turn build, got %d calls", rendered)
	}
}

// D1 core: with NO renderer installed the assembled prompt is byte-
// identical to the pre-tree shape. Pin the exact no-moment expectation:
// the prompt still assembles (anchor present) and contains no moment
// block; the stable hash equals a loop that never had the seam at all.
func TestMomentChatLaneNilRendererInert(t *testing.T) {
	baseline := NewAgentLoop("moment-chat-baseline", "/tmp")
	basePrompt := baseline.assembleSystemPrompt(momentWStableBuilder())
	baseHash := baseline.LastStablePrefixHash()

	wired := NewAgentLoop("moment-chat-nil", "/tmp")
	wired.SetMomentRenderer(nil) // explicit nil = inert (daemon disabled path)
	got := wired.assembleSystemPrompt(momentWStableBuilder())

	if strings.Contains(got, "# Moment") {
		t.Fatalf("nil renderer must add no moment section: %q", got)
	}
	if got != basePrompt {
		t.Fatalf("nil-renderer prompt not byte-identical to baseline:\n baseline: %q\n got:      %q", basePrompt, got)
	}
	if wired.LastStablePrefixHash() != baseHash {
		t.Fatal("nil-renderer stable hash differs from baseline")
	}
}

// Empty render (daemon has no employee def yet) adds no section.
func TestMomentChatLaneEmptyRenderOmitsSection(t *testing.T) {
	loop := NewAgentLoop("moment-chat-empty-render", "/tmp")
	loop.SetMomentRenderer(func() string { return "" })
	builder := momentWStableBuilder()
	got := loop.assembleSystemPrompt(builder)
	if strings.Contains(got, "# Moment") {
		t.Fatalf("empty render must omit the section: %q", got)
	}
	for _, s := range builder.customSections {
		if s.title == "moment" {
			t.Fatal("empty render must not add an empty moment section")
		}
	}
}

func sectionTitles(b *PromptBuilder) []string {
	titles := make([]string, len(b.customSections))
	for i, s := range b.customSections {
		titles[i] = s.title
	}
	return titles
}

// --- §4 voice lane --------------------------------------------------------

// Override path: buildSystemPrompt routes through
// buildSystemPromptWithOverride (the voice bypass); with a renderer the
// output is override + "\n\n" + block, override still leading.
func TestMomentVoiceLaneOverrideAppend(t *testing.T) {
	override := "You are Ada. Stay terse. [voice persona override]"
	loop := NewAgentLoop("moment-voice-append", "/tmp", WithAgentConfig(AgentConfig{
		SystemPromptOveride: override,
	}))
	loop.SetMomentRenderer(func() string { return momentWBlockV1 })

	got := loop.buildSystemPrompt()
	want := override + "\n\n" + momentWBlockV1
	if got != want {
		t.Fatalf("voice override append mismatch:\n got:  %q\nwant: %q", got, want)
	}
}

// Renderer set + GlobalRules set: the moment block is the LAST section
// (appended after the legacy Global Rules suffix); override still leads.
func TestMomentVoiceLaneAppendOrderWithGlobalRules(t *testing.T) {
	override := "persona override"
	rules := "never fabricate"
	loop := NewAgentLoop("moment-voice-rules", "/tmp", WithAgentConfig(AgentConfig{
		SystemPromptOveride: override,
		GlobalRules:         rules,
	}))
	loop.SetMomentRenderer(func() string { return momentWBlockV1 })

	got := loop.buildSystemPrompt()
	if !strings.HasPrefix(got, override) {
		t.Errorf("override must lead: %q", got)
	}
	if !strings.Contains(got, "## Global Rules") || !strings.Contains(got, rules) {
		t.Errorf("legacy Global Rules suffix lost: %q", got)
	}
	if wantTail := "\n\n" + momentWBlockV1; !strings.HasSuffix(got, wantTail) {
		t.Errorf("moment block must be appended last (want suffix %q) in %q", wantTail, got)
	}
}

// D1 golden: WITHOUT a renderer the override path is byte-identical to
// today's shipped behavior. Legacy shape pinned as literals (mirrors
// loop.go buildSystemPromptWithOverride pre-tree): bare override when
// GlobalRules empty; override + "\n\n## Global Rules\n\n" + rules
// otherwise. These literals are the contract — an impl change here is a
// regression, not a refactor.
func TestMomentVoiceLaneD1GoldenNoRenderer(t *testing.T) {
	cases := []struct {
		name     string
		override string
		rules    string
		want     string
	}{
		{
			name:     "bare override",
			override: "You are Ada. Stay terse.",
			want:     "You are Ada. Stay terse.",
		},
		{
			name:     "override plus global rules (legacy concatenation)",
			override: "persona override",
			rules:    "never fabricate",
			want:     "persona override" + "\n\n## Global Rules\n\n" + "never fabricate",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			baseline := NewAgentLoop("moment-d1-baseline-"+tc.name, "/tmp", WithAgentConfig(AgentConfig{
				SystemPromptOveride: tc.override,
				GlobalRules:         tc.rules,
			}))
			got := baseline.buildSystemPrompt()
			if got != tc.want {
				t.Fatalf("pre-tree baseline must equal the pinned golden:\n got:  %q\nwant: %q", got, tc.want)
			}

			// Explicit nil renderer keeps the same bytes.
			nilLoop := NewAgentLoop("moment-d1-nil-"+tc.name, "/tmp", WithAgentConfig(AgentConfig{
				SystemPromptOveride: tc.override,
				GlobalRules:         tc.rules,
			}))
			nilLoop.SetMomentRenderer(nil)
			if g := nilLoop.buildSystemPrompt(); g != tc.want {
				t.Fatalf("nil-renderer voice prompt not byte-identical (D1):\n got:  %q\nwant: %q", g, tc.want)
			}
			// A renderer installed AFTER a turn still switches the lane on
			// for subsequent builds (no sticky state).
			nilLoop.SetMomentRenderer(func() string { return momentWBlockV1 })
			if g := nilLoop.buildSystemPrompt(); !strings.HasSuffix(g, "\n\n"+momentWBlockV1) {
				t.Fatalf("renderer installed later must take effect on the next build: %q", g)
			}
		})
	}
}

// The renderer must NOT leak into non-override chat turns when the config
// carries both an override and no builder path... i.e. one renderer, two
// lanes, no double-append: the chat-lane prompt contains the block exactly
// once per build.
func TestMomentBlockAppearsOncePerPrompt(t *testing.T) {
	loop := NewAgentLoop("moment-single-append", "/tmp", WithAgentConfig(AgentConfig{
		SystemPromptOveride: "override persona",
	}))
	loop.SetMomentRenderer(func() string { return momentWBlockV1 })
	got := loop.buildSystemPrompt()
	if n := strings.Count(got, "# Moment"); n != 1 {
		t.Fatalf("moment block rendered %d times per prompt, want exactly 1:\n%q", n, got)
	}
}
