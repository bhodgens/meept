//go:build e2e

package tuiflows

import (
	"bytes"
	"context"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/tui"
)

// tuiDriver owns one headless TUI App running against the harness daemon:
// the real entry-point constructor (tui.NewApp) under a bubbletea v2
// Program with in-memory I/O — no TTY. bubbletea v2's Send blocks on an
// unbuffered channel until the single-threaded event loop receives the
// message, so sequential sends are ordered and every Update completes
// before the next message is handled (the internal/tui headless pattern).
//
// All model reads happen on the FINISHED app (finish() quits the program
// and waits for p.Run) — reading the model while the loop runs would race
// it. finish() also re-applies the fixed window size: bubbletea sends its
// own startup WindowSizeMsg{0,0} from a goroutine that can land after any
// scripted message; Run returning means that message was processed, so
// re-applying is final (single-goroutine Update, race-free).
type tuiDriver struct {
	app     *tui.App
	program *tea.Program
	done    chan struct{}
	out     *bytes.Buffer
	w, h    int
}

// tuiOptions customizes driver construction.
type tuiOption func(*tuiDriverConfig)

type tuiDriverConfig struct {
	targetSession string
	w, h          int
}

// WithTargetSession pre-programs the session to load on startup (the
// CLI --session path: SetTargetSession must be set before the program's
// Init runs loadSession).
func WithTargetSession(id string) tuiOption {
	return func(c *tuiDriverConfig) { c.targetSession = id }
}

// WithSize fixes the headless window size (default 100x30).
func WithSize(w, h int) tuiOption {
	return func(c *tuiDriverConfig) { c.w, c.h = w, h }
}

// startTUIAgainst attaches the TUI to an existing harness stack.
func startTUIAgainst(t testing.TB, stack *harness.Stack, opts ...tuiOption) *tuiDriver {
	t.Helper()
	cfg := tuiDriverConfig{w: 100, h: 30}
	for _, o := range opts {
		o(&cfg)
	}
	// NewApp resolves client config from $HOME/.meept/client.json5; the
	// test binary must see the sandboxed home too.
	t.Setenv("HOME", stack.Home)
	t.Setenv("MEEPT_HOME", stack.MeeptHome)

	app := tui.NewApp(stack.SocketPath, stack.ProjectDir)
	if cfg.targetSession != "" {
		app.SetTargetSession(cfg.targetSession)
	}

	var out bytes.Buffer
	var in bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	p := tea.NewProgram(app,
		tea.WithContext(ctx),
		tea.WithInput(&in),
		tea.WithOutput(&out),
	)

	d := &tuiDriver{app: app, program: p, done: make(chan struct{}), out: &out, w: cfg.w, h: cfg.h}
	go func() {
		_, _ = p.Run()
		close(d.done)
	}()
	t.Cleanup(d.quitSync)

	// Fixed window size: the foundation of deterministic assertions.
	d.program.Send(tea.WindowSizeMsg{Width: d.w, Height: d.h})
	d.settle()
	return d
}

// sendKey sends one key press ("ctrl+x", "enter", "down", "a", ...).
func (d *tuiDriver) sendKey(key string) {
	d.program.Send(keyPressMsg(key))
}

// typeText sends one KeyPressMsg per rune WITH Text set — the shape the
// bubbles textarea v2 inserts from (its default case inserts msg.Text;
// a Text-less rune key inserts nothing). tui-steer-01 needs this to
// drive real chat input through the TUI's own typing path.
func (d *tuiDriver) typeText(text string) {
	for _, r := range text {
		d.program.Send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

// settle sends a trailing window message as an ordering barrier: when it
// returns, every earlier message has been processed by the loop.
func (d *tuiDriver) settle() {
	d.program.Send(tea.WindowSizeMsg{Width: d.w, Height: d.h})
}

// finish quits the program, waits for p.Run, re-applies the fixed size
// (see the type comment), and returns the final model.
func (d *tuiDriver) finish() *tui.App {
	d.quitSync()
	d.app.Update(tea.WindowSizeMsg{Width: d.w, Height: d.h})
	return d.app
}

func (d *tuiDriver) quitSync() {
	d.program.Quit()
	select {
	case <-d.done:
	case <-time.After(5 * time.Second):
	}
}

// keyPressMsg translates a key name into a bubbletea v2 KeyPressMsg —
// the subset the flows drive (ctrl combos, named keys, single runes).
func keyPressMsg(key string) tea.KeyPressMsg {
	switch key {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "escape", "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	}
	if rest, ok := cutPrefix(key, "ctrl+"); ok && len(rest) == 1 {
		return tea.KeyPressMsg{Code: rune(rest[0]), Mod: tea.ModCtrl}
	}
	if len(key) == 1 {
		return tea.KeyPressMsg{Code: rune(key[0])}
	}
	return tea.KeyPressMsg{Code: rune(key[0])}
}

func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return s, false
}
