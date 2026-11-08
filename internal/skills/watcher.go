package skills

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
	"github.com/fsnotify/fsnotify"
)

// TopicSkillChange is the typed bus topic fired after a hot-reload commit.
// Payload is SkillChanged; declared here (internal/skills) so the compiler
// enforces payload agreement between this publisher and any TUI/GUI
// subscriber. Name matches the gate test's raw topic string exactly
// (typed and raw publishes to the same name interoperate).
var TopicSkillChange = bus.NewTopic[SkillChanged]("skills/change")

// SkillChanged is the payload for TopicSkillChange ("skills/change").
// Source identifies the emitting subsystem; Names lists the affected skill
// names when they are known cheaply (they may be empty for a bulk reload).
type SkillChanged struct {
	Source string   `json:"source"`
	Names  []string `json:"names,omitempty"`
}

// skillChangeDebounce collapses fsnotify bursts (editors write + rename,
// `git checkout` touches many files) into one reload pass. Ported from the
// dsh watcher's stability threshold (skill-filesystem/index.ts:294-615).
const skillChangeDebounce = 200 * time.Millisecond

// SkillWatcherConfig configures NewSkillWatcher.
type SkillWatcherConfig struct {
	// Roots are the skill discovery root directories to watch (typically
	// the project `.meept/skills` and user `~/.meept/skills` tiers).
	// Missing roots are skipped at Start; a nil/empty list is an error.
	Roots []string
	// Loader is the LazySkillLoader whose cache is invalidated on changes.
	// Nil is allowed (a watch-only watcher that still publishes the bus
	// event), but the normal wiring passes the live loader.
	Loader *LazySkillLoader
	// Discover re-runs discovery and returns fresh metadata entries for the
	// index rebuild. May be nil: then only loader invalidation + the bus
	// event fire (the gate tests exercise that path).
	Discover func(context.Context) ([]*SkillIndexEntry, error)
	// Index receives the rebuilt metadata from Discover. Nil Discover/nil
	// Index together mean "invalidate + publish only".
	Index *SkillIndex
	// Bus receives the skills/change event. Nil disables publishing.
	Bus *bus.MessageBus
	// Logger receives watcher diagnostics; nil defaults to slog.Default().
	Logger *slog.Logger
}

// SkillWatcher watches skill discovery roots for changes and hot-invalidates
// the LazySkillLoader cache. Only canonical skill files count as relevant:
// `<root>/<skill>/SKILL.md` (bundle layout) and `<root>/<skill>.md` (flat
// layout) — stray events (`.git`, editor locks, `README.md` inside a bundle)
// never trigger a reload (dsh 1b `isRelevantWatchEvent` parity).
type SkillWatcher struct {
	cfg    SkillWatcherConfig
	logger *slog.Logger
	fw     *fsnotify.Watcher

	mu     sync.Mutex
	closed bool
	// pending accumulates affected skill names during a debounce window.
	pending map[string]bool
	// timer fires the coalesced reload; replaced on every relevant event.
	timer *time.Timer
	// done is closed when the event loop exits.
	done chan struct{}
}

// NewSkillWatcher creates a watcher over cfg.Roots and immediately arms it:
// the fsnotify watches are registered here (existing roots + existing skill
// bundle dirs one level down) and the event loop goroutine is spawned, so a
// watcher fires without an explicit Start call. The background context
// covers the loop's lifetime; Start remains available for ctx-controlled
// shutdown and re-arm, and Close stops everything.
func NewSkillWatcher(cfg SkillWatcherConfig) (*SkillWatcher, error) {
	if len(cfg.Roots) == 0 {
		return nil, errors.New("skills: watcher requires at least one root")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &SkillWatcher{
		cfg:     cfg,
		logger:  logger,
		fw:      fw,
		pending: make(map[string]bool),
		done:    make(chan struct{}),
	}
	if err := w.watchRoots(context.Background()); err != nil {
		_ = fw.Close()
		return nil, err
	}
	go w.loop(context.Background())
	return w, nil
}

// Start re-arms the watcher under the given context: it (re)adds the fsnotify
// watches and spawns a fresh event loop that also honors ctx cancellation.
// Calling Start after construction is optional (the constructor already
// armed the watcher with a background context); calling it twice spawns a
// second loop — prefer Start for ctx-controlled shutdown, Close to stop.
func (w *SkillWatcher) Start(ctx context.Context) error {
	if err := w.watchRoots(ctx); err != nil {
		return err
	}
	go w.loop(ctx)
	return nil
}

// watchRoots registers fsnotify watches for the roots that exist today plus
// their existing skill bundle dirs one level down.
//
// In addition to each root, existing skill bundle directories
// (`<root>/<skill>/`) are watched directly: on macOS kqueue and some other
// backends fsnotify does not report file creation inside an unwatched
// subdirectory, so a brand-new `SKILL.md` in an existing bundle would
// otherwise never surface. New directories created under a root are added
// dynamically in handleEvent.
func (w *SkillWatcher) watchRoots(ctx context.Context) error {
	for _, root := range w.cfg.Roots {
		if root == "" {
			continue
		}
		// Skip roots that do not exist today (contract: watch only the
		// roots that EXIST today).
		if err := w.fw.Add(root); err != nil {
			w.logger.Debug("skill watcher: skipping missing root",
				"root", root, "error", err)
			continue
		}
		// Watch existing bundle dirs one level down for their SKILL.md.
		dirEntries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, de := range dirEntries {
			if !de.IsDir() {
				continue
			}
			bundle := filepath.Join(root, de.Name())
			if err := w.fw.Add(bundle); err != nil {
				w.logger.Debug("skill watcher: skipping bundle dir",
					"dir", bundle, "error", err)
			}
		}
	}
	return nil
}

// Close stops the event loop and releases the fsnotify resources. Safe to
// call more than once.
func (w *SkillWatcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	timer := w.timer
	w.timer = nil
	w.mu.Unlock()

	if timer != nil {
		timer.Stop()
	}
	err := w.fw.Close()
	// The loop goroutine exits when fw.Close makes the events channel
	// return; done stays open until loop() returns.
	<-w.done
	return err
}

// loop is the fsnotify event consumer. Relevant events are debounced
// (skillChangeDebounce) into a single reload pass; irrelevant events are
// dropped without rescheduling the timer.
func (w *SkillWatcher) loop(ctx context.Context) {
	defer close(w.done)
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-w.fw.Events:
			if !ok {
				return
			}
			w.handleEvent(ctx, event)
		case err, ok := <-w.fw.Errors:
			if !ok {
				return
			}
			w.logger.Warn("skill watcher: fsnotify error", "error", err)
		}
	}
}

// handleEvent classifies one fsnotify event. A relevant event accumulates
// its skill name and (re)arms the debounce timer. A directory CREATE under a
// watched root is added to the watch list (new skill bundles) so a
// SKILL.md written into it right afterwards is seen.
func (w *SkillWatcher) handleEvent(ctx context.Context, event fsnotify.Event) {
	if event.Has(fsnotify.Create) && isDir(event.Name) {
		// New bundle directory under a root; watch it for SKILL.md before
		// classifying: the gate's write pattern is MkdirAll+WriteFile with
		// no gap, so the SKILL.md write can land before the watch does. The
		// stat below covers that race: an already-present
		// <bundle>/SKILL.md counts as a relevant add for the new bundle.
		_ = w.fw.Add(event.Name)
		skillMD := filepath.Join(event.Name, "SKILL.md")
		if _, err := os.Stat(skillMD); err == nil {
			w.accumulate(filepath.Base(event.Name))
			w.armDebounce(ctx)
		}
		return
	}
	name, relevant := w.relevantSkillChange(event)
	if !relevant {
		return
	}
	w.accumulate(name)
	w.armDebounce(ctx)
}

// armDebounce (re)arms the coalesced-reload timer under the watcher mutex.
func (w *SkillWatcher) armDebounce(ctx context.Context) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.timer = time.AfterFunc(skillChangeDebounce, func() {
		w.reload(ctx)
	})
	w.mu.Unlock()
}

// accumulate records one affected skill name for the next coalesced reload
// (the debounce timer must already be armed or is armed right after).
func (w *SkillWatcher) accumulate(name string) {
	w.mu.Lock()
	if !w.closed {
		w.pending[name] = true
	}
	w.mu.Unlock()
}

// isDir reports whether path is an existing directory.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// relevantSkillChange reports whether event touches a canonical skill file
// and, if so, returns the affected skill name. Canonical paths:
// `<root>/<skill>/SKILL.md` and `<root>/<skill>.md`.
func (w *SkillWatcher) relevantSkillChange(event fsnotify.Event) (string, bool) {
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return "", false
	}
	dir, base := filepath.Split(filepath.Clean(event.Name))
	dir = filepath.Clean(dir)
	base = strings.ToLower(base)

	if base == "skill.md" {
		// Bundle layout: the skill name is the containing directory, and
		// that directory must sit DIRECTLY under a watched root (the gate
		// pins "only the canonical paths"; a stray SKILL.md deeper in a
		// linked-assets tree must not fire).
		slug := filepath.Base(dir)
		if slug == "." || slug == "/" || !w.watchedRoot(dir) {
			return "", false
		}
		return slug, true
	}
	if strings.HasSuffix(base, ".md") && w.watchedRoot(dir) {
		// Flat layout: `<skill>.md` directly in the root.
		return strings.TrimSuffix(base, ".md"), true
	}
	return "", false
}

// watchedRoot reports whether dir sits directly under one of the watcher's
// roots.
func (w *SkillWatcher) watchedRoot(dir string) bool {
	return slices.Contains(w.cfg.Roots, dir)
}

// reload runs the coalesced reload pass: invalidate the loader cache for the
// affected skills, rebuild the index from fresh discovery, then publish
// skills/change AFTER the durable commit.
func (w *SkillWatcher) reload(ctx context.Context) {
	w.mu.Lock()
	names := make([]string, 0, len(w.pending))
	for n := range w.pending {
		names = append(names, n)
	}
	w.pending = make(map[string]bool)
	w.mu.Unlock()

	if len(names) == 0 {
		return
	}
	sort.Strings(names)

	// 1. Invalidate the loader cache (scoped where possible; a full Clear
	// only when we cannot attribute the change).
	if w.cfg.Loader != nil {
		if w.cfg.Discover != nil {
			// A re-discover reindexes everything anyway; SetIndex clears
			// the cache wholesale on the success path.
			w.cfg.Loader.Clear()
		} else {
			for _, n := range names {
				w.cfg.Loader.Invalidate(n)
			}
		}
	}

	// 2. Re-discover + rebuild the index (metadata-only rescan).
	if w.cfg.Discover != nil && w.cfg.Index != nil {
		entries, err := w.cfg.Discover(ctx)
		if err != nil {
			w.logger.Warn("skill watcher: rediscover failed",
				"error", err)
			w.cfg.Index.MarkDirty()
		} else {
			w.cfg.Index.IndexAll(entries)
			if w.cfg.Loader != nil {
				w.cfg.Loader.SetIndex(w.cfg.Index)
			}
		}
	} else if w.cfg.Index == nil && w.cfg.Loader != nil && len(names) > 0 {
		// No separate Index wired: the loader owns its own index. Index each
		// affected name into the loader's CURRENT index from its canonical
		// path (bundle: <root>/<name>/SKILL.md, flat: <root>/<name>.md),
		// trying both layouts; whichever parses wins. The loader's index
		// pointer is re-set (SetIndex) so its cache clears for the fresh
		// lookup. A removed skill leaves a stale entry + MarkDirty visible.
		idx := w.cfg.Loader.Index()
		if idx != nil {
			idx.MarkDirty()
			for _, root := range w.cfg.Roots {
				for _, n := range names {
					for _, p := range []string{
						filepath.Join(root, n, "SKILL.md"),
						filepath.Join(root, n+".md"),
					} {
						entry, err := ParseSkillMetadataOnly(p)
						if err != nil || entry == nil {
							continue
						}
						idx.Index(entry)
						break
					}
				}
			}
			w.cfg.Loader.SetIndex(idx)
		}
	}

	// 3. Publish AFTER the durable commit (contract: post-change, not
	// before). PublishExternalOnly: TUI/GUI refresh is fire-and-forget; an
	// absent subscriber is normal, not a wiring bug.
	if w.cfg.Bus != nil {
		// Fire-and-forget publish with the typed payload shape
		// {source:"skills", names:[...]}.
		payload := SkillChanged{Source: "skills", Names: names}
		w.cfg.Bus.PublishExternalOnly(TopicSkillChange.Name, skillChangedMessage(payload))
	}
}

// skillChangedMessage wraps a SkillChanged payload in a bus message.
func skillChangedMessage(payload SkillChanged) *models.BusMessage {
	data, err := json.Marshal(payload)
	if err != nil {
		// json.Marshal of this struct cannot fail (strings + slice of
		// strings); guarded for robustness.
		data = []byte(`{"source":"skills"}`)
	}
	return &models.BusMessage{
		Type:      models.MessageTypeEvent,
		Source:    "skills",
		Timestamp: time.Now().UTC(),
		Payload:   data,
	}
}
