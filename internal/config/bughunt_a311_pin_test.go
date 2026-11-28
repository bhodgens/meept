// bughunt 2026-11-25 gap A3-11 pin test.
package config

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// A3-11: every schema-declared `string` field that holds duration-looking
// values must be registered in stringDurationKeys, or a quoted "30m" is
// rewritten to a nanosecond integer and json.Unmarshal fails the string
// field. This test (a) round-trips the full registered set including the
// scheduler.one_shot_window member the audit found unregistered, and (b)
// fails on ANY schema string field named after a duration shape that is
// NOT in the registry — the trap the file warns about, now enforced.
func TestStringDurationKeys_A311_OneShotWindowAndSchemaSweep(t *testing.T) {
	// (a) round-trip through the real loader path: exempt keys keep their
	// quoted strings; genuine time.Duration fields still convert.
	src := `{
		"one_shot_window": "30m",
		"interactive_window": "5m",
		"poll_interval": "15m",
		"reconcile_interval": "1h",
		"refresh_interval": "30m",
		"refresh_margin": "5m",
		"idle_timeout": "5m",
		"periodic_interval": 30s,
		"genuine_ns": 45
	}`
	out := preprocessDurations(src)

	var parsed struct {
		OneShotWindow     string `json:"one_shot_window"`
		InteractiveWindow string `json:"interactive_window"`
		PollInterval      string `json:"poll_interval"`
		ReconcileInterval string `json:"reconcile_interval"`
		RefreshInterval   string `json:"refresh_interval"`
		RefreshMargin     string `json:"refresh_margin"`
		IdleTimeout       string `json:"idle_timeout"`
		PeriodicInterval  int64  `json:"periodic_interval"`
		GenuineNS         int64  `json:"genuine_ns"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("unmarshal preprocessed config: %v\noutput: %s", err, out)
	}
	for name, got := range map[string]string{
		"one_shot_window":    parsed.OneShotWindow,
		"interactive_window": parsed.InteractiveWindow,
		"poll_interval":      parsed.PollInterval,
		"reconcile_interval": parsed.ReconcileInterval,
		"refresh_interval":   parsed.RefreshInterval,
		"refresh_margin":     parsed.RefreshMargin,
		"idle_timeout":       parsed.IdleTimeout,
	} {
		if got == "" {
			t.Errorf("%s: empty/rewritten — exempt key must survive as its string value (output: %s)", name, out)
		}
	}
	if parsed.OneShotWindow != "30m" {
		t.Errorf("one_shot_window = %q, want %q (A3-11: unregistered → rewritten to nanoseconds)", parsed.OneShotWindow, "30m")
	}
	// Genuine duration-typed fields still convert in both passes.
	if parsed.PeriodicInterval != int64(30*time.Second) {
		t.Errorf("periodic_interval = %d, want %d (duration conversion broken)", parsed.PeriodicInterval, int64(30*time.Second))
	}
	if parsed.GenuineNS != 45 {
		t.Errorf("genuine_ns = %d, want 45 (plain int mangled)", parsed.GenuineNS)
	}
}

// TestStringDurationKeys_A311_FullSchemaCoverage is the reflection-driven
// tripwire for the A3-11 trap class: it walks the LIVE Config schema for
// every `string`-typed field whose json tag has a duration shape
// (interval/duration/window/ttl/timeout/expiry/margin/retention) and
// asserts each one is registered in stringDurationKeys. A future
// string-duration field added to the schema without registering it fails
// here at CI time instead of bricking user config load.
func TestStringDurationKeys_A311_FullSchemaCoverage(t *testing.T) {
	found := map[string]bool{}
	walkStringDurationFields(t, reflect.TypeOf(Config{}), found, map[reflect.Type]bool{})
	if len(found) == 0 {
		t.Fatal("schema sweep found zero string-duration fields — the shape filter is broken (the pin would be vacuous)")
	}
	// The seven known members must all still be discovered.
	for _, known := range []string{
		"poll_interval", "reconcile_interval", "idle_timeout",
		"one_shot_window", "interactive_window",
		"refresh_interval", "refresh_margin",
	} {
		if !found[known] {
			t.Errorf("schema sweep missed known string-duration field %q (filter regression)", known)
		}
	}
	for key := range found {
		if !stringDurationKeys[key] {
			t.Errorf("schema string field %q holds duration-looking values but is NOT registered in stringDurationKeys — preprocessDurations would rewrite its quoted value to an integer (A3-11)", key)
		}
	}
}

var durShapeKey = regexp.MustCompile(`(?i)(interval|duration|window|_ttl|^ttl|timeout|expiry|margin|retention|period|every|cooldown|backoff|stale_after|max_age|grace|delay|wait)`)

// walkStringDurationFields recursively collects json-tag names of string
// fields whose names look like durations. Recursion is type-guarded
// (cycles through pointer/shared structs cannot loop forever).
//
// Bughunt 2026-11-27 S-9/F12: the walk previously recursed only through
// Ptr/Slice/Struct, so a duration-string nested under a map-of-struct
// config section (lsp.servers.*, providers.*, agents.backoff.*) was
// invisible, and the shape regex missed period/every/cooldown/backoff/
// stale_after/max_age/grace/delay/wait. Map VALUES that resolve to
// structs are now walked too.
func walkStringDurationFields(t *testing.T, typ reflect.Type, out map[string]bool, seen map[reflect.Type]bool) {
	t.Helper()
	for typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || seen[typ] {
		return
	}
	seen[typ] = true
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		ft := f.Type
		for ft.Kind() == reflect.Ptr || ft.Kind() == reflect.Slice {
			ft = ft.Elem()
		}
		// Map-of-struct config sections (S-9): walk the VALUE type so a
		// string-duration field inside e.g. map[string]ServerConfig is
		// discovered exactly like a nested struct field. (Note: unlike
		// the struct-field recursion below, there is NO PkgPath guard
		// here — for a named struct type PkgPath is the package path,
		// not an unexported marker, so the guard would skip every
		// same-package map leaf.)
		if ft.Kind() == reflect.Map {
			mt := ft.Elem()
			for mt.Kind() == reflect.Ptr {
				mt = mt.Elem()
			}
			if mt.Kind() == reflect.Struct {
				walkStringDurationFields(t, mt, out, seen)
			}
		}
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		if f.Type.Kind() == reflect.String && durShapeKey.MatchString(tag) {
			out[tag] = true
		}
		if ft.Kind() == reflect.Struct && ft.PkgPath() != "" {
			walkStringDurationFields(t, ft, out, seen)
		}
	}
}
