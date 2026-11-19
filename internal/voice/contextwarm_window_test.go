package voice

// Context-warm separate-window gate test (plan tree
// 20261118-voice-tts-stt-optimizations/04-context-warm/00T-warm-tests.md,
// task T1; feature F7, contracts C1/R4).
//
// Contract pinned here (branch orchestrator C1 + 01-separate-window.md):
//
//	ContextWarmWindowMs int // 0 = resolved turn_confirm_ms (legacy); >0 = override debounce window (ms)
//	OnFinal cancellation unchanged.
//
// Given override 300; When RoutePartial; Then the timer fires at the
// override window. Given 0; Then it fires at the resolved turn_confirm_ms
// (900 ms default).
//
// RED style (spec-first test-leaf convention): the contract field does not
// exist yet, so every reference to it is through REFLECTION, never a direct
// Go field access (which would be a compile error and break the whole
// package's test build). This file compiles today and fails RED on the
// assertion (missing symbol), turning GREEN the moment the field lands —
// with zero edits to this file. t.Skip is deliberately not used: a gate
// test that skips is a gate failure.
//
// CONTRACT-GAPS (G-1): the window RESOLUTION itself is computed in the
// unexported daemon wiring (VoiceWiring.newContextWarmer), which package
// voice cannot call. The behavioral subtests therefore drive the public
// RoutePartial path with a window resolved from the pinned config field,
// while the strict RED is the pinned config-field assertion below. The
// daemon-integration half is covered by the e2e context_warm suite (leaf
// 01-separate-window.md T2). Neither ambiguity weakens an assertion here.

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// cfgIntField reads an int field off a config.VoiceConfig via reflection.
// ok=false means the contract symbol is missing; the caller names it in
// its failure message.
func cfgIntField(v reflect.Value, name string) (int, bool) {
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if !v.IsValid() {
		return 0, false
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.Int {
		return 0, false
	}
	return int(f.Int()), true
}

// setCfgInt writes an int field on an addressable config.VoiceConfig via
// reflection. ok=false means the contract symbol is missing.
func setCfgInt(v reflect.Value, name string, val int) bool {
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if !v.IsValid() {
		return false
	}
	f := v.FieldByName(name)
	if !f.IsValid() || f.Kind() != reflect.Int || !f.CanSet() {
		return false
	}
	f.SetInt(int64(val))
	return true
}

// resolvedWarmWindow mirrors the C1 resolution contract over the pinned
// config field: a positive ContextWarmWindowMs overrides the debounce
// window in milliseconds; 0 falls back to the resolved turn_confirm_ms,
// which itself falls back to the warmer's 900 ms default. This is the
// strictest reading of the contract; the RED is the missing field, not
// this helper's arithmetic.
func resolvedWarmWindow(t *testing.T, vc *config.VoiceConfig) time.Duration {
	t.Helper()
	override, ok := cfgIntField(reflect.ValueOf(vc), "ContextWarmWindowMs")
	if !ok {
		t.Fatal("missing symbol config.VoiceConfig.ContextWarmWindowMs (F7/C1): " +
			"separate context-warm window knob absent (0 = turn_confirm_ms legacy, >0 = override ms)")
	}
	if override > 0 {
		return time.Duration(override) * time.Millisecond
	}
	confirm, ok := cfgIntField(reflect.ValueOf(vc), "TurnConfirmMs")
	if !ok {
		t.Fatal("missing symbol config.VoiceConfig.TurnConfirmMs (startguard C1): legacy window source absent")
	}
	if confirm <= 0 {
		return time.Duration(DefaultContextWarmWindowMs) * time.Millisecond
	}
	return time.Duration(confirm) * time.Millisecond
}

// windowTestWarmer builds a live warmer that reports each dispatched warm
// on fires. The prefix clears the token floor so a partial always warms.
func windowTestWarmer(window time.Duration, fires chan<- struct{}) *ContextWarmer {
	return NewContextWarmer(ContextWarmConfig{
		Enabled:   true,
		MinTokens: 60,
		Window:    window,
	}, ContextWarmDeps{
		Warm: func(context.Context, string, []llm.ChatMessage, string) {
			select {
			case fires <- struct{}{}:
			default:
			}
		},
		BuildPrefix: func(_, sess, partial string) (WarmPrefix, bool) {
			return WarmPrefix{
				Messages: []llm.ChatMessage{
					{Role: llm.RoleSystem, Content: strings.Repeat("x", 400)},
					{Role: llm.RoleUser, Content: partial},
				},
				Alias: "warm-window-test",
				Scope: sess,
			}, true
		},
		BaseCtx: context.Background(),
	})
}

// TestContextWarmWindowOverride is the F7 gate: the separate window knob
// exists (RED today) and, once it lands, changes the RoutePartial debounce
// from the legacy turn_confirm_ms to the override value.
func TestContextWarmWindowOverride(t *testing.T) {
	// --- Part A: the pinned C1/C2 config field (strict RED today). ---
	cfgType := reflect.TypeOf(config.VoiceConfig{})
	field, ok := cfgType.FieldByName("ContextWarmWindowMs")
	if !ok {
		t.Fatal("missing symbol config.VoiceConfig.ContextWarmWindowMs (F7/C1): " +
			"0 = resolved turn_confirm_ms (legacy); >0 = override debounce window in ms")
	}
	if field.Type.Kind() != reflect.Int {
		t.Errorf("config.VoiceConfig.ContextWarmWindowMs is %s, want int (F7/C1)", field.Type.Kind())
	}
	if got := field.Tag.Get("json"); got != "context_warm_window_ms" {
		t.Errorf("config.VoiceConfig.ContextWarmWindowMs json tag = %q, want %q (C2)", got, "context_warm_window_ms")
	}
	var zero config.VoiceConfig
	if got, ok := cfgIntField(reflect.ValueOf(&zero), "ContextWarmWindowMs"); !ok || got != 0 {
		t.Errorf("zero-value ContextWarmWindowMs = %d (found=%v), want 0 (legacy = turn_confirm_ms)", got, ok)
	}

	// --- Part B: Given/When/Then over the public RoutePartial path. ---

	// Given override 60 ms; When RoutePartial; Then the warm fires inside
	// the short override window (well before the 900 ms legacy window).
	t.Run("override_shortens_debounce", func(t *testing.T) {
		vc := config.VoiceConfig{}
		if !setCfgInt(reflect.ValueOf(&vc), "ContextWarmWindowMs", 60) {
			t.Fatal("missing symbol config.VoiceConfig.ContextWarmWindowMs (F7/C1): cannot set override")
		}
		window := resolvedWarmWindow(t, &vc)
		if window != 60*time.Millisecond {
			t.Fatalf("resolved window with override 60 = %v, want 60ms", window)
		}

		fires := make(chan struct{}, 4)
		cw := windowTestWarmer(window, fires)
		SetContextWarmRouting(cw)
		defer func() {
			SetContextWarmRouting(nil)
			cw.OnCallEnd("ccid-warm-override")
		}()

		RoutePartial("ccid-warm-override", "emp", "sess-override", "override probe partial text")
		select {
		case <-fires:
			// warm landed inside the override window
		case <-time.After(700 * time.Millisecond):
			t.Fatal("override window (60ms) produced no warm within 700ms (RoutePartial debounce broken)")
		}
	})

	// Given 0; When RoutePartial; Then the warm uses the resolved
	// turn_confirm_ms (900 ms default) and does NOT fire inside the
	// shorter override-sized window.
	t.Run("zero_uses_turn_confirm_ms", func(t *testing.T) {
		vc := config.VoiceConfig{} // ContextWarmWindowMs = 0 (legacy)
		window := resolvedWarmWindow(t, &vc)
		if want := time.Duration(DefaultContextWarmWindowMs) * time.Millisecond; window != want {
			t.Fatalf("legacy window with override 0 = %v, want %v (resolved turn_confirm_ms default)", window, want)
		}

		fires := make(chan struct{}, 4)
		cw := windowTestWarmer(window, fires)
		SetContextWarmRouting(cw)
		defer func() {
			SetContextWarmRouting(nil)
			cw.OnCallEnd("ccid-warm-legacy")
		}()

		RoutePartial("ccid-warm-legacy", "emp", "sess-legacy", "legacy probe partial text")
		select {
		case <-fires:
			t.Fatal("legacy window (900ms) fired prematurely: override leaked into the zero-value path")
		case <-time.After(300 * time.Millisecond):
			// no warm inside 300ms: the 900ms legacy window is in force
		}
	})
}
