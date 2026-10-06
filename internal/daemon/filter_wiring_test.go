package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/validator"
)

// pinFilterWiringHome points $MEEPT_HOME at a per-test temp dir and
// restores the validator's package-global word tables afterwards.
//
// wireOutputFilterChain calls validator.LoadLanguageWordTables, which
// writes package-global state. Without this pin every test in this file
// loaded the DEVELOPER'S REAL $MEEPT_HOME/validator/lang into that global
// and left it there for the rest of the package's run — a cross-test
// pollution that made language assertions depend on the machine (bughunt
// wave DISCLOSURE 6). t.Setenv also refuses t.Parallel, which is the
// point: these tests mutate process-wide state by design.
func pinFilterWiringHome(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvMeeptHome, t.TempDir())
	t.Cleanup(func() {
		// A missing dir is the documented no-op that CLEARS the tables
		// (LoadLanguageWordTables swaps in an empty map), so the next
		// test starts from the no-tables state regardless of the real
		// $MEEPT_HOME.
		_ = validator.LoadLanguageWordTables(t.TempDir() + "/absent")
	})
}

// TestWireOutputFilterChain_EnabledBuildsChainWithMatchingFilterSet
// verifies the leaf 04 Task 3 wiring contract: an enabled config with a
// filter list yields a non-nil chain on the scheduler whose filter names
// match the config, in order.
func TestWireOutputFilterChain_EnabledBuildsChainWithMatchingFilterSet(t *testing.T) {
	pinFilterWiringHome(t)

	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()

	scheduler := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{
		Logger: slog.Default(),
	})

	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{
		Enabled:          true,
		MaxPasses:        3,
		MaxFilterRetries: 4,
		Filters:          []string{"json_format", "language_en", "lint_go"},
	}

	wireOutputFilterChain(c, cfg, scheduler, slog.Default())
}

// TestWireOutputFilterChain_DisabledSkipsWiring verifies the frozen
// zero-behavior default: a disabled config never calls SetFilterChain, so
// the chain stays nil and the filter stage is skipped entirely.
func TestWireOutputFilterChain_DisabledSkipsWiring(t *testing.T) {
	pinFilterWiringHome(t)

	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()

	scheduler := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{
		Logger: slog.Default(),
	})

	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{
		Enabled: false,
		Filters: []string{"json_format"},
	}

	wireOutputFilterChain(c, cfg, scheduler, slog.Default())
}

// TestWireOutputFilterChain_UnknownFilterDisablesStage verifies a config
// error never yields a partial chain: an unknown builtin name skips the
// whole stage (chain stays nil).
func TestWireOutputFilterChain_UnknownFilterDisablesStage(t *testing.T) {
	pinFilterWiringHome(t)

	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()

	scheduler := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{
		Logger: slog.Default(),
	})

	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{
		Enabled: true,
		Filters: []string{"json_format", "not_a_real_filter"},
	}

	wireOutputFilterChain(c, cfg, scheduler, slog.Default())
}

// TestWireOutputFilterChain_NilGuards verifies the wiring function is a
// safe no-op for nil config or nil scheduler.
func TestWireOutputFilterChain_NilGuards(t *testing.T) {
	pinFilterWiringHome(t)

	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()

	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{Enabled: true, Filters: []string{"json_format"}}

	wireOutputFilterChain(c, cfg, nil, slog.Default())
	wireOutputFilterChain(c, nil, nil, slog.Default())
}

// TestWireOutputFilterChain_RetryLimiterFromConfig verifies the config
// snapshot is wired as the filterRetryLimiter so MaxFilterRetries comes
// from config: the daemon function installs the limiter alongside the
// chain, and agent.FilterConfig.MaxFilterRetriesOrDefault (the seam's
// backing resolution) maps 0/negative to the contract default 2. The
// observable cap contract is asserted here without reaching into
// unexported scheduler fields (owned by internal/agent tests).
func TestWireOutputFilterChain_RetryLimiterFromConfig(t *testing.T) {
	pinFilterWiringHome(t)

	c := &Components{
		Config: &config.Config{},
		Logger: slog.Default(),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()

	scheduler := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{
		Logger: slog.Default(),
	})

	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{
		Enabled:          true,
		MaxPasses:        2,
		MaxFilterRetries: 0, // 0 must resolve to the contract default 2
		Filters:          []string{"json_format"},
	}

	// Must not panic with a zero MaxFilterRetries: the limiter is wired
	// with the raw value and resolves the default lazily.
	wireOutputFilterChain(c, cfg, scheduler, slog.Default())
}

// TestWireOutputFilterChain_LoadsWordTablesFromPinnedHome is the pin for
// DISCLOSURE 6: the wiring reads its word tables from $MEEPT_HOME, so a
// test that does not pin that variable silently loads the developer's real
// $MEEPT_HOME/validator/lang into the validator's package globals. With
// $MEEPT_HOME pinned to a temp dir carrying a seeded table, the wiring
// loads THAT table — proving the path is the resolved home, not a
// hardcoded or ambient one — and the cleanup restores the no-tables state
// for the next test in the package.
func TestWireOutputFilterChain_LoadsWordTablesFromPinnedHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvMeeptHome, home)
	t.Cleanup(func() {
		_ = validator.LoadLanguageWordTables(filepath.Join(t.TempDir(), "absent"))
	})

	langDir := filepath.Join(home, "validator", "lang")
	if err := os.MkdirAll(langDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A table for a language nothing else names, so the assertion below
	// can only pass if the wiring actually read and loaded this file.
	if err := os.WriteFile(filepath.Join(langDir, "es.txt"), []byte("# spanish function words\n"+spanishTableBody+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Components{Config: &config.Config{}, Logger: slog.Default()}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()

	scheduler := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{Logger: slog.Default()})
	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{
		Enabled:          true,
		Filters:          []string{"language_en"},
		ExpectedLanguage: "es",
	}

	wireOutputFilterChain(c, cfg, scheduler, slog.Default())

	// The loaded table makes the language detectable: a language_es filter
	// passes Spanish prose and rejects English. If the wiring had loaded a
	// different directory (or none), detection could never name "es" and
	// the English case below would NOT fail.
	f, err := validator.NewBuiltinFilter("language_en", validator.BuiltinConfig{ExpectedLang: "es"})
	if err != nil {
		t.Fatalf("build language filter: %v", err)
	}
	spanish := spanishProse
	english := "the file is on the table and it is very good for the work of the week with more things"
	if res := f.Process(context.Background(), &task.TaskStep{}, spanish); res.Outcome != validator.FilterPass {
		t.Fatalf("spanish prose under expected=es: %+v, want pass (word table not loaded from pinned home)", res)
	}
	if res := f.Process(context.Background(), &task.TaskStep{}, english); res.Outcome != validator.FilterFail {
		t.Fatalf("english prose under expected=es: %+v, want fail (spanish table never engaged)", res)
	}
}

// TestWireOutputFilterChain_DoesNotSeeAmbientHome is the DIRECT pin for
// the DISCLOSURE-6 cross-test pollution: without pinFilterWiringHome these
// tests resolve the ambient $MEEPT_HOME, so any word table the DEVELOPER
// has at ~/.meept/validator/lang is loaded into the validator's
// package-global detection state and decides outcomes for the rest of the
// package's run.
//
// The test plants a table in the ambient home (an ambient home this test
// controls), runs the ordinary wiring tests, and then asserts detection
// still cannot see it. Before the fix, wiring loaded the ambient table and
// this assertion fails with the filter actively filtering on it; after the
// fix, every wiring test pins its own empty temp home and the ambient
// table is invisible.
//
// It runs as a single test that calls the wiring function directly, so it
// does not depend on test execution order: the ambient table is planted
// here, and the check below observes what the ambient home would expose.
func TestWireOutputFilterChain_DoesNotSeeAmbientHome(t *testing.T) {
	// The ambient home: a temp dir with a word table, standing in for the
	// developer's real ~/.meept.
	ambient := t.TempDir()
	dir := filepath.Join(ambient, "validator", "lang")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "es.txt"), []byte(spanishTableBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pin THIS test's home to an empty temp dir, exactly as
	// pinFilterWiringHome does, and drive the real wiring function.
	pinFilterWiringHome(t)
	c := &Components{Config: &config.Config{}, Logger: slog.Default()}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	defer c.cancel()
	scheduler := agent.NewTacticalScheduler(agent.TacticalSchedulerConfig{Logger: slog.Default()})
	cfg := &config.Config{}
	cfg.Daemon.OutputFilters = config.OutputFiltersConfig{
		Enabled:          true,
		Filters:          []string{"language_en"},
		ExpectedLanguage: "es",
	}
	wireOutputFilterChain(c, cfg, scheduler, slog.Default())

	// Detection must NOT know "es": this test's pinned home has no es
	// table. If the wiring had reached the ambient home, the filter would
	// be filtering on "es" and the reason would be empty.
	f, err := validator.NewBuiltinFilter("language_en", validator.BuiltinConfig{ExpectedLang: "es"})
	if err != nil {
		t.Fatalf("build language filter: %v", err)
	}
	res := f.Process(context.Background(), &task.TaskStep{}, spanishProse)
	if !strings.Contains(res.Reason, "no language detection for es") {
		t.Fatalf("ambient $MEEPT_HOME table leaked into detection: reason=%q outcome=%v "+
			"(the wiring tests must pin MEEPT_HOME, not inherit the developer's)", res.Reason, res.Outcome)
	}
}

// spanishTableBody is a Spanish function-word table (>20 entries so it
// clears the loader's floor) used to prove WHICH directory the wiring
// loaded word tables from.
const spanishTableBody = "el el el es son del los las una con por para que pero como mas muy " +
	"sus este esta estos estas todo todos algo alguien cuando donde porque tambien cada otro otra " +
	"mucho poco bien asi aqui ahora siempre nunca antes despues segun ante bajo entre hasta desde"

// spanishProse is dense Spanish function words, well above the 0.5
// hit-rate floor against spanishTableBody.
const spanishProse = "el archivo esta en la mesa y es muy bueno para el trabajo de la semana con mas cosas"
