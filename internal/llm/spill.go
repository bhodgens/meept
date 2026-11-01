package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/pathutil"
	"github.com/caimlas/meept/pkg/id"
)

// Spill pointer line format. The pointer replaces the oversized tool output in
// conversation history; the file on disk holds the raw (unwrapped) content so
// the model can re-read it with the file tool if needed.
const spillPointerFormat = "[Output spilled to %s — re-read with the file tool if needed (%d tokens)]"

// spillSessionIDAllowSet is the rune set allowed in the session-id path
// component ([a-zA-Z0-9-_], root open question #4 default). Anything else
// (including '/' and '.', which enable traversal) is replaced with '_'.
const spillSessionIDAllowSet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"

// sanitizeSpillSessionID maps a session id onto the path-safe allow set so the
// spill directory can never escape the spill root via traversal or odd runes.
func sanitizeSpillSessionID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if strings.ContainsRune(spillSessionIDAllowSet, r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// SpillService writes oversized tool outputs to disk under
// $MEEPT_HOME/spill/<session_id>/ so pruneToolOutputs can leave a re-fetch
// pointer instead of irreversibly truncating (Manus rule: drop content only
// when a pointer + re-fetch affordance remain).
//
// No goroutines, no locks: every Spill call is independent file I/O.
type SpillService struct {
	// spillRoot is the spill directory root. Empty = resolve lazily via
	// meeptHomeOverride-aware default ($MEEPT_HOME/spill).
	spillRoot string

	// meeptHomeOverride pins the meept home for testing; empty = use
	// MEEPT_HOME / ~/.meept (mirrors config.MeeptHome; internal/llm cannot
	// import internal/config — import cycle).
	meeptHomeOverride string

	logger *slog.Logger
}

// NewSpillService builds a SpillService rooted at $MEEPT_HOME/spill.
// meeptHome overrides the home directory resolution (tests, rigs); empty
// means the standard MEEPT_HOME / ~/.meept resolution. Nil-receiver-safe.
func NewSpillService(meeptHome string) *SpillService {
	return &SpillService{meeptHomeOverride: meeptHome, logger: slog.Default()}
}

// resolveSpillRoot returns the spill root directory.
func (s *SpillService) resolveSpillRoot() string {
	if s != nil && s.spillRoot != "" {
		return s.spillRoot
	}
	home := strings.TrimSpace(os.Getenv(pathutil.EnvMeeptHome))
	if s != nil && s.meeptHomeOverride != "" {
		home = s.meeptHomeOverride
	}
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".meept")
		} else {
			home = ".meept" // pathological env; matches config.MeeptHome fallback
		}
	} else if strings.HasPrefix(home, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, home[2:])
		}
	}
	return filepath.Join(home, "spill")
}

// Spill writes content (the RAW, unwrapped tool output) to
// <spillRoot>/<sanitized session id>/<sha256(content)[:16]>.txt and returns
// the pointer line that replaces the tool output in conversation history.
// MkdirAll 0700, file 0600. No goroutines.
func (s *SpillService) Spill(ctx context.Context, sessionID, content string) (string, error) {
	root := s.resolveSpillRoot()
	dir := filepath.Join(root, sanitizeSpillSessionID(sessionID))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("spill: mkdir %s: %w", dir, err)
	}

	sum := sha256.Sum256([]byte(content))
	path := filepath.Join(dir, hex.EncodeToString(sum[:])[:16]+".txt")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("spill: write %s: %w", path, err)
	}

	tokens := countSpillTokens(content)
	return fmt.Sprintf(spillPointerFormat, path, tokens), nil
}

// countSpillTokens estimates the token count for the pointer line using the
// same 3-chars-per-token heuristic as HeuristicTokenizer (the spill service
// has no Tokenizer dependency; the estimate is advisory for the model).
func countSpillTokens(content string) int {
	if content == "" {
		return 0
	}
	n := len(content)
	tokens := (n + 2) / 3 // ceil(n/3), matching HeuristicTokenizer
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

// toolSpillRoot returns the tool-layer spill root <home>/spill/tools,
// honoring the service's meeptHomeOverride (tests) and MEEPT_HOME env. The
// tool layer lives under a distinct /tools subdirectory so its TTL policy
// (leaf 02's SweepToolSpills) can differ from the compaction spill tree.
func (s *SpillService) toolSpillRoot() string {
	return filepath.Join(s.resolveSpillRoot(), "tools")
}

// sanitizeToolSpillSessionDir maps a session id onto the path-safe component
// of a tool-spill directory. An empty session id becomes "nosession" (the
// shell tool's spill dir for session-less runs); any other id is run through
// the standard spill allow-set so it can never escape the root.
func sanitizeToolSpillSessionDir(id string) string {
	sid := sanitizeSpillSessionID(id)
	if sid == "" {
		return "nosession"
	}
	return sid
}

// WriteToolSpill writes the FULL raw (stdout+stderr as captured, no
// re-encoding) tool output to
// <home>/spill/tools/<session_id|nosession>/<id>.txt and returns the
// absolute path. MkdirAll 0700, file 0600. The filename id comes from
// pkg/id.Generate (never time.Now().UnixNano / math/rand — repo invariant,
// predid analyzer). No goroutines, no locks: every call is independent I/O.
//
// Callers (the shell tool) treat this as BEST-EFFORT: on error they log a
// warning and omit the spill pointer, but the tool call itself still
// succeeds with its tail truncation. A failure here is never fatal to a
// command.
func (s *SpillService) WriteToolSpill(sessionID string, data []byte) (string, error) {
	dir := filepath.Join(s.toolSpillRoot(), sanitizeToolSpillSessionDir(sessionID))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("tool spill: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, id.Generate("")+".txt")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", fmt.Errorf("tool spill: write %s: %w", path, err)
	}
	return path, nil
}

// sweepInFlightGuard is the mtime window (relative to `now`) inside which a
// tool-spill file is considered in-flight: a spill written by a running
// tool call may still be re-read by the model, so no sweep branch — age or
// size — may ever delete a file newer than this window.
const sweepInFlightGuard = time.Hour

// spillSweepEntry is one tool-spill file collected for a sweep pass.
type spillSweepEntry struct {
	path  string
	mtime time.Time
	size  int64
}

// DefaultToolSpillRoot returns the tool-spill root: $MEEPT_HOME/spill/tools
// (MEEPT_HOME-aware, mirroring how SpillService.resolveSpillRoot resolves
// $MEEPT_HOME/spill). The path convention under the root is
// <session_id>/<ulid>.txt (plan C2).
func DefaultToolSpillRoot() string {
	home := strings.TrimSpace(os.Getenv(pathutil.EnvMeeptHome))
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".meept")
		} else {
			home = ".meept" // pathological env; matches config.MeeptHome fallback
		}
	} else if strings.HasPrefix(home, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, home[2:])
		}
	}
	return filepath.Join(home, "spill", "tools")
}

// SweepToolSpills walks root (the tool-spill tree, convention
// $MEEPT_HOME/spill/tools/<session_id>/<ulid>.txt) and removes spill files
// that are no longer needed. Pure function: no daemon imports, no globals,
// deterministic in its inputs.
//
// Passes, in order:
//
//  1. Age: every file with mtime < now-maxAge is deleted.
//  2. Size: while the total size of the remaining files exceeds maxBytes,
//     the oldest-mtime files are deleted first (ties broken by path, so
//     the eviction order is deterministic: sort by (mtime, path)).
//
// In-flight protection: a file whose mtime is within 1h of now is NEVER
// deleted by either pass — its spill may still be re-read by the model.
// When the size pass can reach the cap only by deleting protected files,
// it stops and returns the removed count; the overflow stays (a later
// sweep re-tries once the files age out).
//
// Individual per-file remove/stat failures are skipped (they surface on
// the next sweep); only a failure to read the tree is returned, wrapped.
// The caller logs the removed count; errors here are never fatal to a
// daemon startup.
func SweepToolSpills(root string, maxAge time.Duration, maxBytes int64, now time.Time) (removed int, err error) {
	entries, err := collectSpillFiles(root)
	if err != nil {
		return 0, fmt.Errorf("spill sweep: walk %s: %w", root, err)
	}

	// Pass 1: age. The in-flight guard applies under BOTH passes: a
	// file within 1h of now is never deleted, even though a 24h maxAge
	// makes that overlap impossible with the daemon constants.
	ageCutoff := now.Add(-maxAge)
	for _, e := range entries {
		if e.mtime.Before(ageCutoff) && now.Sub(e.mtime) >= sweepInFlightGuard {
			if removeSpillFile(e.path) {
				removed++
			}
		}
	}

	// Pass 2: size cap, oldest-first with a deterministic (mtime, path)
	// order. Rebuild the survivor list (pass 1 removed some entries) and
	// its total size.
	remaining := make([]spillSweepEntry, 0, len(entries))
	for _, e := range entries {
		if _, statErr := os.Stat(e.path); statErr == nil {
			remaining = append(remaining, e)
		}
	}
	total := int64(0)
	for _, e := range remaining {
		total += e.size
	}
	if total <= maxBytes {
		return removed, nil
	}

	sort.Slice(remaining, func(i, j int) bool {
		if !remaining[i].mtime.Equal(remaining[j].mtime) {
			return remaining[i].mtime.Before(remaining[j].mtime)
		}
		return remaining[i].path < remaining[j].path
	})
	for _, e := range remaining {
		if total <= maxBytes {
			break
		}
		// In-flight guard: never delete a file newer than 1h.
		if now.Sub(e.mtime) < sweepInFlightGuard {
			continue
		}
		if os.Remove(e.path) == nil {
			total -= e.size
			removed++
		}
	}
	return removed, nil
}

// collectSpillFiles walks root and returns every regular file with its
// mtime and size. A missing root is not an error (no tool spills yet);
// any other walk failure is. No lock is held across this I/O — the
// function is pure and re-entrant; concurrent sweeps simply see
// whatever state the filesystem reports.
func collectSpillFiles(root string) ([]spillSweepEntry, error) {
	var entries []spillSweepEntry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil // raced with another sweep / removal
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			if errors.Is(statErr, fs.ErrNotExist) {
				return nil
			}
			return statErr
		}
		entries = append(entries, spillSweepEntry{
			path:  path,
			mtime: info.ModTime(),
			size:  info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// removeSpillFile deletes a path; a raced ErrNotExist counts as success
// (another sweep got there first).
func removeSpillFile(path string) bool {
	err := os.Remove(path)
	return err == nil || errors.Is(err, fs.ErrNotExist)
}
