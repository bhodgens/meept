package daemon

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/agent/turnjournal"
	"github.com/caimlas/meept/internal/config"
)

// tjCaptureHandler implements slog.Handler, recording "LEVEL message"
// lines for assertions. Named to avoid the existing captureHandler.
type tjCaptureHandler struct {
	lines []string
}

func (h *tjCaptureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *tjCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	h.lines = append(h.lines, r.Level.String()+" "+r.Message)
	return nil
}
func (h *tjCaptureHandler) WithAttrs(attrs []slog.Attr) slog.Handler { return h }
func (h *tjCaptureHandler) WithGroup(name string) slog.Handler       { return h }
func (h *tjCaptureHandler) count(substr string) int {
	n := 0
	for _, line := range h.lines {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}
func (h *tjCaptureHandler) countLevel(level slog.Level) int {
	n := 0
	for _, line := range h.lines {
		if strings.HasPrefix(line, level.String()+" ") {
			n++
		}
	}
	return n
}

// constructTurnJournal mirrors the NewComponents construction block so
// the tests exercise the same decision tree as production.
func constructTurnJournal(c *Components, cfg *config.Config, logger *slog.Logger) {
	if cfg.Daemon.TurnJournalEnabled {
		turnJournalDB := filepath.Join(cfg.Daemon.DataDir, "turnjournal.db")
		turnJournal, tjErr := turnjournal.NewSQLiteJournal(turnJournalDB, logger)
		if tjErr != nil {
			logger.Warn("Failed to create turn journal -- turns will NOT be journaled", "error", tjErr)
		} else {
			c.TurnJournal = turnJournal
		}
	} else {
		logger.Info("turn journal disabled (daemon.turn_journal_enabled=false)")
	}
}

// T5: flag off -> no store, no db file, one Info log.
func TestTurnJournalWiringFlagOff(t *testing.T) {
	dir := t.TempDir()
	h := &tjCaptureHandler{}
	logger := slog.New(h)
	cfg := &config.Config{Daemon: config.DaemonConfig{DataDir: dir, TurnJournalEnabled: false}}

	c := &Components{}
	constructTurnJournal(c, cfg, logger)

	if c.TurnJournal != nil {
		t.Fatal("flag off: TurnJournal = non-nil, want nil")
	}
	if _, err := os.Stat(filepath.Join(dir, "turnjournal.db")); !os.IsNotExist(err) {
		t.Errorf("flag off: turnjournal.db exists (stat err %v), want no file", err)
	}
	if n := h.count("turn journal disabled"); n != 1 {
		t.Errorf("Info lines = %d, want exactly one 'turn journal disabled' (lines: %v)", n, h.lines)
	}
	if n := h.countLevel(slog.LevelWarn); n != 0 {
		t.Errorf("flag off: %d Warn lines, want 0", n)
	}
}

// T5: flag on + store ok -> non-nil SQLite store; hooks build over it
// and the processor accepts them.
func TestTurnJournalWiringFlagOn(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Daemon: config.DaemonConfig{DataDir: dir, TurnJournalEnabled: true}}

	c := &Components{}
	constructTurnJournal(c, cfg, slog.New(slog.DiscardHandler))

	if c.TurnJournal == nil {
		t.Fatal("flag on: TurnJournal = nil, want the SQLite store")
	}
	if _, ok := c.TurnJournal.(*turnjournal.SQLiteJournal); !ok {
		t.Fatalf("store type = %T, want *turnjournal.SQLiteJournal", c.TurnJournal)
	}
	// The db file was created (schema migrated).
	if _, err := os.Stat(filepath.Join(dir, "turnjournal.db")); err != nil {
		t.Errorf("flag on: turnjournal.db not created: %v", err)
	}
	// The hooks build over the store and the processor accepts them.
	hooks := agent.NewTurnJournalHooks(c.TurnJournal, discardLogger())
	if hooks == nil {
		t.Fatal("hooks = nil over a live store")
	}
	p, _, _ := newTestAgentJobProcessor(t)
	p.WithTurnJournal(hooks)

	if err := c.TurnJournal.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

// T5: a failed open (data dir is a FILE) -> nil store + Warn, no panic.
func TestTurnJournalWiringStoreFails(t *testing.T) {
	// A path under a FILE cannot host a database.
	fileBlocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(fileBlocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &tjCaptureHandler{}
	cfg := &config.Config{Daemon: config.DaemonConfig{DataDir: fileBlocker, TurnJournalEnabled: true}}

	c := &Components{}
	constructTurnJournal(c, cfg, slog.New(h))

	if c.TurnJournal != nil {
		t.Fatal("failed open: TurnJournal = non-nil, want nil (best-effort disable)")
	}
	if n := h.countLevel(slog.LevelWarn); n == 0 {
		t.Errorf("no Warn line on failed open (lines: %v)", h.lines)
	}
}
