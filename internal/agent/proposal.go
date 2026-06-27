package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ReflectionProposal represents a single proposed improvement surfaced by the
// reflection system (or by a manual /remember invocation).
type ReflectionProposal struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"` // skill_create|skill_update|agent_prompt|project_instruction|prompt_component
	Target        string    `json:"target"`
	Change        string    `json:"change"`
	Justification string    `json:"justification"`
	Confidence    float64   `json:"confidence"`
	Source        string    `json:"source"` // turn:sessionID | session:sessionID | manual:/remember
	Status        string    `json:"status"` // pending|applied|skipped
	CreatedAt     time.Time `json:"created_at"`
}

// proposalQueue appends ReflectionProposals to a markdown file and parses
// pending entries back out. The file format is human-readable and append-only:
//
//	## [pending] 2026-06-25 — <id>
//	- **Type:** <type>
//	- **Target:** <target>
//	- **Confidence:** 0.80
//	- **Source:** <source>
//	- **Justification:** <text>
//	- **Proposed change:** <text>
//
// MarkApplied / MarkSkipped rewrite the header in place.
type proposalQueue struct {
	path string
	mu   sync.Mutex // serializes read-modify-write in markStatus against Append
}

func newProposalQueue(path string) *proposalQueue {
	return &proposalQueue{path: path}
}

// Append writes a new proposal to the queue file. ID, Status, and CreatedAt
// are filled in if zero.
//
// Concurrency: the mutex serializes this Append against markStatus's
// read-truncate-write. Without this mutex, markStatus's os.WriteFile could
// truncate a proposal that Append just wrote (TOCTOU race: markStatus reads
// the file, then Append writes, then markStatus overwrites with its stale
// in-memory copy — the new proposal is lost). The file I/O happens while
// holding the lock; this is an intentional exception to the CLAUDE.md
// mutex-scope rule because the alternative (collect under lock, release,
// operate) would reintroduce the race we are fixing.
func (q *proposalQueue) Append(p ReflectionProposal) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if p.ID == "" {
		p.ID = generateProposalID()
	}
	if p.Status == "" {
		p.Status = "pending"
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	// Build the full block as one string so a single Write call is atomic.
	block := fmt.Sprintf(
		"\n## [%s] %s — %s\n- **Type:** %s\n- **Target:** %s\n- **Confidence:** %.2f\n- **Source:** %s\n- **Justification:** %s\n- **Proposed change:** %s\n",
		p.Status, p.CreatedAt.Format("2006-01-02"), p.ID,
		p.Type, p.Target, p.Confidence, p.Source,
		p.Justification,
		strings.ReplaceAll(p.Change, "\n", "\n  "),
	)
	if err := os.MkdirAll(filepath.Dir(q.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(q.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(block)
	return err
}

// ListPending reads the queue file and returns all proposals with status "pending".
func (q *proposalQueue) ListPending() ([]ReflectionProposal, error) {
	data, err := os.ReadFile(q.path) //nolint:mutexio // I/O under mutex required to prevent race with Append
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	all := parseProposals(string(data))
	var pending []ReflectionProposal
	for _, p := range all {
		if p.Status == "pending" || p.Status == "" {
			pending = append(pending, p)
		}
	}
	return pending, nil
}

// MarkApplied updates a proposal's status from "pending" to "applied" with timestamp.
func (q *proposalQueue) MarkApplied(id string) error {
	return q.markStatus(id, "applied")
}

// MarkSkipped updates a proposal's status from "pending" to "skipped" with timestamp.
func (q *proposalQueue) MarkSkipped(id string) error {
	return q.markStatus(id, "skipped")
}

func (q *proposalQueue) markStatus(id, newStatus string) error {
	// markStatus does read-modify-write on the queue file: it reads the whole
	// file, replaces a [pending] header in memory, then truncates and rewrites
	// the entire file via os.WriteFile. The mutex serializes markStatus calls
	// against both markStatus and Append so the truncate-write cannot destroy
	// proposals that a concurrent Append just wrote. The file I/O is performed
	// while holding the lock — this is an intentional exception to the
	// CLAUDE.md mutex-scope rule because the alternative (collect under lock,
	// release, operate) would reintroduce the very race we are fixing.
	q.mu.Lock()
	defer q.mu.Unlock()

	data, err := os.ReadFile(q.path) //nolint:mutexio // I/O under mutex required to prevent race with Append
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	stamp := time.Now().UTC().Format("2006-01-02")
	// The ID lives in the ## [pending] header line itself. Find the header
	// that contains the ID and rewrite its status marker in place.
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, "## [pending]") && strings.Contains(line, id) {
			lines[i] = strings.Replace(
				line,
				"[pending]",
				fmt.Sprintf("[%s %s]", newStatus, stamp),
				1,
			)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("proposal %s not found or not in pending state", id)
	}
	return os.WriteFile(q.path, []byte(strings.Join(lines, "\n")), 0o644) //nolint:mutexio // I/O under mutex required to prevent race with Append
}

// isAlwaysProposeOnly returns true for files that must never be auto-applied
// regardless of reflection.auto_apply_all config. CLAUDE.md, AGENT.md, and
// anything under config/prompts/ are always propose-only.
func isAlwaysProposeOnly(target string) bool {
	clean := filepath.Clean(target)
	if clean == "CLAUDE.md" {
		return true
	}
	if strings.HasPrefix(clean, "config/agents/") && strings.HasSuffix(clean, "AGENT.md") {
		return true
	}
	if strings.HasPrefix(clean, "config/prompts/") {
		return true
	}
	return false
}

// isSafeTargetPath returns true if the target path is safe for auto-apply.
// It rejects absolute paths, paths with ".." traversal components, and paths
// that escape the working directory. This prevents a malicious or buggy
// proposal from writing to arbitrary filesystem locations (e.g., /etc/cron.d/
// or ../../.ssh/authorized_keys).
func isSafeTargetPath(target string) bool {
	clean := filepath.Clean(target)
	// Reject absolute paths.
	if filepath.IsAbs(clean) {
		return false
	}
	// Reject paths that traverse above the working directory.
	if strings.HasPrefix(clean, "../") || clean == ".." {
		return false
	}
	// Reject any path component that is "..".
	for _, part := range strings.Split(filepath.ToSlash(clean), "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

// IsSafeTargetPath is the exported wrapper around isSafeTargetPath for
// external callers (TUI, HTTP handlers).
func IsSafeTargetPath(target string) bool {
	return isSafeTargetPath(target)
}

func generateProposalID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp if rand fails (shouldn't happen in practice)
		return fmt.Sprintf("p%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ProposalQueueExternal is the exported wrapper around proposalQueue for
// cross-package use (CLI, HTTP handlers, RememberTool, TUI /remember). It
// exposes only the operations external callers need while keeping the
// internal queue type sealed.
type ProposalQueueExternal struct {
	inner *proposalQueue
}

// NewExternalProposalQueue creates an external wrapper around the queue at the
// given path. The path's parent directories are created on first Append.
func NewExternalProposalQueue(path string) *ProposalQueueExternal {
	return &ProposalQueueExternal{inner: newProposalQueue(path)}
}

// Append writes a new proposal to the queue. ID, Status, and CreatedAt are
// filled in if zero (same semantics as the internal queue).
func (q *ProposalQueueExternal) Append(p ReflectionProposal) error {
	return q.inner.Append(p)
}

// ListPending returns all proposals with status "pending".
func (q *ProposalQueueExternal) ListPending() ([]ReflectionProposal, error) {
	return q.inner.ListPending()
}

// MarkApplied marks the proposal with the given ID as applied.
func (q *ProposalQueueExternal) MarkApplied(id string) error {
	return q.inner.MarkApplied(id)
}

// MarkSkipped marks the proposal with the given ID as skipped.
func (q *ProposalQueueExternal) MarkSkipped(id string) error {
	return q.inner.MarkSkipped(id)
}

// GenerateProposalID returns a random hex-encoded proposal ID. Exposed so
// cross-package callers (RememberTool, CLI improvements commands) can
// pre-assign an ID before calling ProposalQueueExternal.Append, allowing them
// to reference the proposal ID after queuing without parsing the file back.
// The internal proposalQueue.Append would assign an ID itself if handed an
// empty one, but since it takes ReflectionProposal by value, the assignment
// is not visible to the caller. Pre-generating via this helper closes that
// gap without changing the internal Append signature.
func GenerateProposalID() string {
	return generateProposalID()
}

// parseProposals does a lenient scan of the queue markdown and extracts
// proposals. Status and ID are pulled from the ## [<status>] <date> — <id> header.
// Continuation lines (indented with 2 spaces) in the Proposed change field are
// joined to reconstruct the original multi-line content.
func parseProposals(content string) []ReflectionProposal {
	var out []ReflectionProposal
	lines := strings.Split(content, "\n")
	var cur *ReflectionProposal
	inChangeContinuation := false
	for _, line := range lines {
		if strings.HasPrefix(line, "## [") {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &ReflectionProposal{}
			inChangeContinuation = false
			// Parse: "## [pending] 2026-06-25 — abc123"
			rest := strings.TrimPrefix(line, "## [")
			// rest = "pending] 2026-06-25 — abc123"
			closeBracket := strings.Index(rest, "]")
			if closeBracket > 0 {
				cur.Status = strings.TrimSpace(rest[:closeBracket])
				// drop the trailing "applied <date>" or "skipped <date>" if present
				cur.Status = strings.Fields(cur.Status)[0]
				afterBracket := rest[closeBracket+1:]
				// afterBracket = " 2026-06-25 — abc123"
				emIdx := strings.Index(afterBracket, "—")
				if emIdx >= 0 {
					cur.ID = strings.TrimSpace(afterBracket[emIdx+len("—"):])
				}
			}
		} else if cur != nil && strings.HasPrefix(line, "- **Type:**") {
			cur.Type = strings.TrimSpace(strings.TrimPrefix(line, "- **Type:**"))
			inChangeContinuation = false
		} else if cur != nil && strings.HasPrefix(line, "- **Target:**") {
			cur.Target = strings.TrimSpace(strings.TrimPrefix(line, "- **Target:**"))
			inChangeContinuation = false
		} else if cur != nil && strings.HasPrefix(line, "- **Confidence:**") {
			fmt.Sscanf(line, "- **Confidence:** %f", &cur.Confidence)
			inChangeContinuation = false
		} else if cur != nil && strings.HasPrefix(line, "- **Source:**") {
			cur.Source = strings.TrimSpace(strings.TrimPrefix(line, "- **Source:**"))
			inChangeContinuation = false
		} else if cur != nil && strings.HasPrefix(line, "- **Justification:**") {
			cur.Justification = strings.TrimSpace(strings.TrimPrefix(line, "- **Justification:**"))
			inChangeContinuation = false
		} else if cur != nil && strings.HasPrefix(line, "- **Proposed change:**") {
			cur.Change = strings.TrimSpace(strings.TrimPrefix(line, "- **Proposed change:**"))
			inChangeContinuation = true
		} else if cur != nil && inChangeContinuation {
			// Continuation line: Append indents with "  " (2 spaces). Strip
			// the indentation and rejoin with newline.
			trimmed := strings.TrimPrefix(line, "  ")
			if trimmed == line {
				// Not indented — this is some other content, not a continuation.
				inChangeContinuation = false
			} else {
				cur.Change += "\n" + trimmed
			}
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}
