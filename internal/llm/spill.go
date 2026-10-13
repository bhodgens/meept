package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/pathutil"
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
