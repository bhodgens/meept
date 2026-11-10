package bot

// 00T-moment gate §5 — plan 20261110-situational leaf 01-moment/00T:
// BotDefinition awareness fields round-trip through the definition JSON
// blob with ZERO schema ALTER (executor-agent precedent,
// executor_agent_test.go: TestExecutorAgentJSONTag +
// TestStore_ExecutorAgentRoundTrip). TESTS ONLY; RED posture expected until
// leaf 01/01 lands.
//
// LOCATION PIN (per gate doc: BotWeather lives in ONE package; this gate
// pins it in internal/bot — the 00T-weather daemon gate's own header pins
// the SAME choice, so sibling files stay consistent): the Go type
// BotWeather is declared in package bot; internal/moment mirrors the data
// via its own EmployeeDef fields and imports neither bot nor config. Impl
// leaf 01/01 follows this pin.
//
// Pinned field shapes (frozen contract block, master.md; JSON tags pinned,
// Go location pinned above):
//
//	type BotWeather struct {
//	    Lat    float64  `json:"lat"`
//	    Lon    float64  `json:"lon"`
//	    Trades []string `json:"trades,omitempty"`
//	}
//	BotDefinition gains:
//	    Timezone        string            `json:"timezone,omitempty"`
//	    LocationLabel   string            `json:"location_label,omitempty"`
//	    Weather         BotWeather        `json:"weather,omitempty"`
//	    PersonaDayState map[string]string `json:"persona_day_state,omitempty"`
//
// omitempty is pinned on all four so a pre-tree definition (all zero)
// serializes byte-identically to today's blob — a non-emptiable zero
// struct/keys would change stored bytes for existing employees. The
// master block writes "Weather BotWeather" (value); the header comment in
// the contract says "nil/zero = no weather awareness" — zero value
// satisfies "absent", so the VALUE form is pinned here and the daemon
// treats Lat==0 && Lon==0 && Trades empty as absent.
//
// Test-local names carry the momentW* prefix so this file coexists with
// sibling gate additions in package bot (none dispatched yet, but pinned).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// JSON tag pin: snake-case keys, omitted when zero.
func TestAwarenessFieldsJSONTags(t *testing.T) {
	t.Parallel()

	full := BotDefinition{
		ID:            "e1",
		Prompt:        "p",
		Timezone:      "America/Denver",
		LocationLabel: "Rapid City shop",
		Weather:       BotWeather{Lat: 44.08, Lon: -103.21, Trades: []string{"concrete", "roofing"}},
		PersonaDayState: map[string]string{
			"behind": "Keep it short, lead with blockers.",
		},
	}
	data, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	for _, want := range []string{
		`"timezone":"America/Denver"`,
		`"location_label":"Rapid City shop"`,
		`"lat":44.08`,
		`"lon":-103.21`,
		`"trades":["concrete","roofing"]`,
		`"persona_day_state":{"behind":"Keep it short, lead with blockers."}`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("marshal missing %s: %s", want, s)
		}
	}

	// Zero fields omit entirely: pre-tree defs serialize byte-identically.
	bare := BotDefinition{ID: "e2", Prompt: "p"}
	data, err = json.Marshal(bare)
	if err != nil {
		t.Fatalf("marshal bare: %v", err)
	}
	for _, banned := range []string{"timezone", "location_label", "weather", "persona_day_state"} {
		if strings.Contains(string(data), banned) {
			t.Errorf("omitempty violated for %q: %s", banned, data)
		}
	}
}

// Store roundtrip: Create -> Get and Update -> Get preserve all four
// fields through the JSON blob with no migration (zero ALTER).
func TestStore_AwarenessFieldsRoundTrip(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	def := testBotDef("moment-aware-1")
	def.Timezone = "America/Denver"
	def.LocationLabel = "Rapid City shop"
	def.Weather = BotWeather{Lat: 44.08, Lon: -103.21, Trades: []string{"concrete"}}
	def.PersonaDayState = map[string]string{"behind": "Keep it terse."}

	if err := store.Create(ctx, def); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := store.Get(ctx, "moment-aware-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	assertAwarenessEqual(t, "create", got, def)

	// Update path: change every field, re-read.
	def.Timezone = "Europe/Berlin"
	def.LocationLabel = "Berlin office"
	def.Weather = BotWeather{Lat: 52.52, Lon: 13.405, Trades: []string{"earthwork"}}
	def.PersonaDayState = map[string]string{"overloaded": "Escalate.", "clear": "Steady."}
	if err := store.Update(ctx, def); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = store.Get(ctx, "moment-aware-1")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	assertAwarenessEqual(t, "update", got, def)

	// Clearing: empty timezone/label + zero weather + nil map round-trip
	// back as zero values (the disabled shape).
	def.Timezone = ""
	def.LocationLabel = ""
	def.Weather = BotWeather{}
	def.PersonaDayState = nil
	if err := store.Update(ctx, def); err != nil {
		t.Fatalf("Update clear: %v", err)
	}
	got, err = store.Get(ctx, "moment-aware-1")
	if err != nil {
		t.Fatalf("Get after clear: %v", err)
	}
	if got.Timezone != "" || got.LocationLabel != "" || got.Weather.Lat != 0 || len(got.Weather.Trades) != 0 || got.PersonaDayState != nil {
		t.Fatalf("cleared awareness did not round-trip zero: %+v", got)
	}
}

func assertAwarenessEqual(t *testing.T, phase string, got *BotDefinition, want BotDefinition) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s: Get returned nil", phase)
	}
	if got.Timezone != want.Timezone {
		t.Errorf("%s: Timezone = %q, want %q", phase, got.Timezone, want.Timezone)
	}
	if got.LocationLabel != want.LocationLabel {
		t.Errorf("%s: LocationLabel = %q, want %q", phase, got.LocationLabel, want.LocationLabel)
	}
	if got.Weather.Lat != want.Weather.Lat || got.Weather.Lon != want.Weather.Lon {
		t.Errorf("%s: Weather coords = %v/%v, want %v/%v", phase, got.Weather.Lat, got.Weather.Lon, want.Weather.Lat, want.Weather.Lon)
	}
	if strings.Join(got.Weather.Trades, ",") != strings.Join(want.Weather.Trades, ",") {
		t.Errorf("%s: Weather.Trades = %v, want %v", phase, got.Weather.Trades, want.Weather.Trades)
	}
	if len(got.PersonaDayState) != len(want.PersonaDayState) {
		t.Fatalf("%s: PersonaDayState len = %d, want %d (%v)", phase, len(got.PersonaDayState), len(want.PersonaDayState), got.PersonaDayState)
	}
	for k, v := range want.PersonaDayState {
		if got.PersonaDayState[k] != v {
			t.Errorf("%s: PersonaDayState[%q] = %q, want %q", phase, k, got.PersonaDayState[k], v)
		}
	}
}

// Zero-ALTER pin: the bot_definitions table stays exactly (id, data,
// created_at, updated_at) — new fields live in the JSON blob only. Read
// the schema back from the fresh store and assert the column set, proving
// no migration sneaks in behind the feature.
func TestStore_AwarenessFieldsZeroSchemaAlter(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	if err := store.Create(ctx, testBotDef("moment-schema-pin")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	rows, err := store.db.Query(`SELECT name FROM pragma_table_info('bot_definitions') ORDER BY name`)
	if err != nil {
		t.Fatalf("schema read: %v", err)
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cols = append(cols, n)
	}
	want := "created_at,data,id,updated_at"
	if got := strings.Join(cols, ","); got != want {
		t.Fatalf("bot_definitions columns drifted (awareness fields must live in the JSON blob, zero ALTER): got %q want %q", got, want)
	}
}
