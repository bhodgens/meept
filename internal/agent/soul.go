package agent

// SOUL.md — user-authored persona for meept.
//
// The soul file is the user's editable persona: it is read into the STABLE
// personality slot of every system prompt (WithPersonality path), so an edit
// changes the prompt the next time one is built. Semantics (design leaf:
// skills/software-development/meept-design/references/soul-md-persona.md):
//
//   - Startup: missing file → seed the shipped default; present but invalid
//     → refuse daemon start (mirrors the employee constitution loader's
//     refuse-at-boot posture).
//   - Runtime: valid change → hot reload; invalid change → keep the last
//     accepted copy and log an error. The daemon never crashes or degrades
//     on a bad edit.
//   - "Invalid" is mechanical only: non-UTF-8, empty, unreadable, or over
//     MaxSoulBytes. Prose quality cannot be validated.

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/fsnotify/fsnotify"

	"github.com/caimlas/meept/internal/config"
)

// MaxSoulBytes caps the accepted soul file size. An oversize file is treated
// as invalid (likely a pasted blob, not a persona).
const MaxSoulBytes = 64 * 1024

// SoulFileName is the soul file's name inside the meept home directory.
const SoulFileName = "SOUL.md"

//go:embed soul_default.md
var defaultSoulMD string

// DefaultSoulMD returns the shipped default soul text.
func DefaultSoulMD() string { return defaultSoulMD }

// SoulPath returns the resolved soul file path (honors MEEPT_HOME).
func SoulPath() string { return config.MeeptPath(SoulFileName) }

// ValidateSoul checks the mechanical validity of soul file content. It
// returns a nil error for valid content and a named reason otherwise.
// Empty content is invalid everywhere: at startup it is almost certainly a
// save-in-progress accident, and at runtime an empty persona is never what
// the user meant.
func ValidateSoul(content []byte) error {
	if len(content) == 0 {
		return errors.New("soul.md is empty")
	}
	if len(content) > MaxSoulBytes {
		return fmt.Errorf("soul.md is %d bytes (max %d)", len(content), MaxSoulBytes)
	}
	if !utf8.Valid(content) {
		return errors.New("soul.md is not valid UTF-8")
	}
	return nil
}

// SeedSoulIfMissing writes the shipped default soul to path when the file
// does not exist. An existing file is never touched. Returns true when a
// seed write happened.
func SeedSoulIfMissing(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(defaultSoulMD), 0o600); err != nil {
		return false, fmt.Errorf("seed %s: %w", path, err)
	}
	return true, nil
}

// LoadSoul reads and validates the soul file at path. It is the startup
// gate: an invalid file refuses the daemon.
func LoadSoul(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	if err := ValidateSoul(content); err != nil {
		return "", err
	}
	return string(content), nil
}

// soulSnapshot is an accepted soul state.
type soulSnapshot struct {
	text     string
	sha256   string
	reloaded time.Time
}

// SoulProvider owns the accepted soul text. It is safe for concurrent use:
// prompt builders read via Current(); the watcher goroutine writes via
// reload paths under the same mutex. A nil *SoulProvider is valid and serves
// empty text, so wiring can be unconditional.
type SoulProvider struct {
	path   string
	logger *slog.Logger

	mu       sync.RWMutex
	snapshot soulSnapshot
	watching bool
	watchErr error // last non-recoverable watcher failure, surfaced by Status

	// reloadHook, when set, fires after every accepted reload. AgentLoop
	// uses it to refresh cached prompt-builder personality text.
	reloadHook func(text string)
}

// NewSoulProvider loads the soul file at path and returns a provider holding
// the accepted text. The caller decides startup policy:
//
//   - Daemon start: SeedSoulIfMissing first, then NewSoulProvider; a
//     non-nil error refuses the start.
//   - Tests/CLI: construct directly; CLI surfaces the error.
func NewSoulProvider(path string, logger *slog.Logger) (*SoulProvider, error) {
	if logger == nil {
		logger = slog.Default()
	}
	text, err := LoadSoul(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(text))
	return &SoulProvider{
		path:   path,
		logger: logger,
		snapshot: soulSnapshot{
			text:     text,
			sha256:   hex.EncodeToString(sum[:]),
			reloaded: time.Now(),
		},
	}, nil
}

// NewSoulProviderFromText builds a provider from in-memory text (tests,
// override paths). No file is read.
func NewSoulProviderFromText(text string) *SoulProvider {
	sum := sha256.Sum256([]byte(text))
	return &SoulProvider{
		logger: slog.Default(),
		snapshot: soulSnapshot{
			text:     text,
			sha256:   hex.EncodeToString(sum[:]),
			reloaded: time.Now(),
		},
	}
}

// SetReloadHook registers a callback fired after each accepted reload (and
// never for the constructor-loaded initial text). Must be called before
// StartWatching.
func (s *SoulProvider) SetReloadHook(fn func(text string)) {
	s.mu.Lock()
	s.reloadHook = fn
	s.mu.Unlock()
}

// Current returns the accepted soul text.
func (s *SoulProvider) Current() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot.text
}

// Status returns diagnostics for `meept soul show`.
func (s *SoulProvider) Status() (path, sha string, reloaded time.Time, watching bool, watchErr error) {
	if s == nil {
		return SoulPath(), "", time.Time{}, false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.path, s.snapshot.sha256, s.snapshot.reloaded, s.watching, s.watchErr
}

// StartWatching begins the fsnotify loop for the soul file. It watches the
// parent directory too: editors (vim, VS Code) save via write-temp-then-
// rename, which surfaces as RENAME/CREATE on the directory rather than a
// WRITE on the original inode. Blocking; run in a goroutine. The context
// cancels the loop (daemon shutdown).
func (s *SoulProvider) StartWatching(ctx context.Context) error {
	if s == nil {
		return errors.New("soul: nil provider")
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("soul: create watcher: %w", err)
	}
	dir := filepath.Dir(s.path)
	// Watch the file (plain writes) and the dir (rename-style saves).
	if err := watcher.Add(s.path); err != nil {
		// Missing file at watch time: keep the current copy; the dir watch
		// re-arms when the file is recreated.
		s.logger.Warn("soul: cannot watch file (will follow recreation)", "path", s.path, "error", err)
	}
	if err := watcher.Add(dir); err != nil {
		if cerr := watcher.Close(); cerr != nil {
			s.logger.Warn("soul: watcher close after dir-watch failure", "path", s.path, "error", cerr)
		}
		return fmt.Errorf("soul: watch dir %s: %w", dir, err)
	}

	s.mu.Lock()
	s.watching = true
	s.mu.Unlock()

	go s.watchLoop(ctx, watcher)
	return nil
}

// watchLoop is the debounce-and-reload pump.
func (s *SoulProvider) watchLoop(ctx context.Context, watcher *fsnotify.Watcher) {
	defer watcher.Close()

	// Debounce: editors emit bursts (WRITE, CHMOD, rename pairs). A timer
	// that keeps sliding gives us one reload per save burst.
	const debounce = 250 * time.Millisecond
	var timer *time.Timer
	fire := make(chan struct{}, 1)

	resetTimer := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(debounce, func() {
			select {
			case fire <- struct{}{}:
			default:
			}
		})
	}

	handle := func(events []fsnotify.Event) {
		for _, ev := range events {
			if ev.Name != s.path && filepath.Dir(ev.Name) != filepath.Dir(s.path) {
				continue
			}
			if ev.Name == s.path || ev.Has(fsnotify.Create) || ev.Has(fsnotify.Rename) {
				// Any write/create/remove on the soul path (or a sibling
				// temp file renamed over it) schedules a re-read.
				if ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename) {
					// File gone: keep serving the last copy. fsnotify drops
					// the inode watch on remove; re-add so a later recreation
					// is still seen (the dir watch covers this either way).
					if aerr := watcher.Add(s.path); aerr != nil {
						s.logger.Debug("soul: re-arm watch after removal", "path", s.path, "error", aerr)
					}
				}
				resetTimer()
			}
		}
	}

	batch := make([]fsnotify.Event, 0, 8)
	for {
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.watching = false
			s.mu.Unlock()
			return
		case ev, ok := <-watcher.Events:
			if !ok {
				return
			}
			batch = append(batch, ev)
			// Drain whatever else is already buffered so one debounce
			// window covers the whole save burst.
		drain:
			for {
				select {
				case ev2, ok2 := <-watcher.Events:
					if !ok2 {
						break drain
					}
					batch = append(batch, ev2)
				default:
					break drain
				}
			}
			handle(batch)
			batch = batch[:0]
		case <-fire:
			s.reread()
		}
	}
}

// reread reads the soul file and applies the runtime policy: valid → swap
// in; invalid → keep the previous copy and log. Transient ENOENT (rename
// window) retries briefly before being accepted as a real removal.
func (s *SoulProvider) reread() {
	var content []byte
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		content, err = os.ReadFile(s.path)
		if err == nil {
			break
		}
		if errors.Is(err, os.ErrNotExist) {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		break
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Genuinely gone: keep last copy.
			s.logger.Warn("soul: file removed, keeping last accepted copy", "path", s.path)
			return
		}
		s.mu.Lock()
		s.watchErr = err
		s.mu.Unlock()
		s.logger.Error("soul: read failed, keeping last accepted copy", "path", s.path, "error", err)
		return
	}

	if verr := ValidateSoul(content); verr != nil {
		s.logger.Error("soul: rejected invalid change, keeping last accepted copy",
			"path", s.path, "reason", verr.Error())
		return
	}

	text := string(content)
	sum := sha256.Sum256(content)

	s.mu.Lock()
	s.snapshot = soulSnapshot{
		text:     text,
		sha256:   hex.EncodeToString(sum[:]),
		reloaded: time.Now(),
	}
	hook := s.reloadHook
	s.mu.Unlock()

	s.logger.Info("soul.md reloaded", "path", s.path, "sha256", sumHex(sum), "bytes", len(content))
	if hook != nil {
		hook(text)
	}
}

func sumHex(sum [sha256.Size]byte) string { return hex.EncodeToString(sum[:]) }
