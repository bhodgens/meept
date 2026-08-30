// WikiStore: persistent wiki layer for learned patterns (arXiv:2608.27454
// WikiSkill §3.1 Wiki Layer). Pattern pages live as markdown under
// <dir>/patterns/ with a hand-rolled YAML frontmatter header in a frozen
// field order; index.md, logs.md, and skill-impact.md are the flat files the
// skill evolver consumes.
//
// Binding constraint (master §Notes): NOTHING in this file may be referenced
// by ContextInjector or any inference prompt builder. The wiki serves the
// evolver only.
package selfimprove

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

const (
	wikiPatternsDir = "patterns"
	wikiIndexFile   = "index.md"
	wikiLogFile     = "logs.md"
	wikiImpactFile  = "skill-impact.md"
)

// SkillImpactEntry is one row of the append-only skill-impact ledger
// (skill-impact.md, JSONL). Field order is frozen — leaf 03 depends on it.
type SkillImpactEntry struct {
	Time      time.Time `json:"time"`
	Action    string    `json:"action"`
	SkillName string    `json:"skill_name"`
	Diff      string    `json:"diff,omitempty"`
	Score     float64   `json:"score"`
	Accepted  bool      `json:"accepted"`
	Reason    string    `json:"reason,omitempty"`
}

// WikiStore persists learned patterns as wiki pages plus the evolver-facing
// flat files. All page writes are atomic (tmp+rename); log and ledger writes
// are append-only. A WikiStore itself performs no locking: LearninPipeline
// serializes its own writes and single-writer use is the only supported mode
// for the flat files.
type WikiStore struct {
	dir    string
	logger *slog.Logger
}

// NewWikiStore creates a WikiStore rooted at dir. A nil logger is allowed.
func NewWikiStore(dir string, logger *slog.Logger) *WikiStore {
	if logger == nil {
		logger = slog.Default()
	}
	return &WikiStore{dir: dir, logger: logger}
}

// patternSlug returns "<domain>-<hash12>" (empty domain → "general"), where
// hash12 is the first 12 hex characters of ContentHash, lowercased. Same
// ContentHash always yields the same slug; the hash is NOT re-hashed.
func patternSlug(p *LearnedPattern) string {
	domain := strings.ToLower(strings.TrimSpace(p.Domain))
	if domain == "" {
		domain = "general"
	}
	hash := strings.ToLower(strings.TrimSpace(p.ContentHash))
	if len(hash) > 12 {
		hash = hash[:12]
	}
	return domain + "-" + hash
}

// UpsertPattern writes the pattern's wiki page, returning its path. Repeated
// upserts of the same ContentHash overwrite the same page with a bumped
// updated_at (the caller is responsible for bumping UpdatedAt).
func (w *WikiStore) UpsertPattern(p *LearnedPattern) (string, error) {
	if p == nil {
		return "", fmt.Errorf("wiki: upsert pattern is nil")
	}
	if err := os.MkdirAll(filepath.Join(w.dir, wikiPatternsDir), 0o755); err != nil {
		return "", fmt.Errorf("wiki: create patterns dir: %w", err)
	}
	pagePath := filepath.Join(w.dir, wikiPatternsDir, patternSlug(p)+".md")

	content := renderPatternPage(p)
	// Atomic write: tmp file in the same directory, then rename.
	tmpPath := pagePath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("wiki: write tmp page: %w", err)
	}
	if err := os.Rename(tmpPath, pagePath); err != nil {
		if rmErr := os.Remove(tmpPath); rmErr != nil {
			w.logger.Warn("wiki: remove tmp page failed", "path", tmpPath, "error", rmErr)
		}
		return "", fmt.Errorf("wiki: rename page: %w", err)
	}
	return pagePath, nil
}

// renderPatternPage renders the frozen page format (see 02-wiki-store.md).
func renderPatternPage(p *LearnedPattern) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "id: %s\n", p.ID)
	fmt.Fprintf(&b, "type: %s\n", p.Type)
	fmt.Fprintf(&b, "status: %s\n", p.Status)
	fmt.Fprintf(&b, "confidence: %s\n", strconv.FormatFloat(p.Confidence, 'f', -1, 64))
	fmt.Fprintf(&b, "use_count: %d\n", p.UseCount)
	fmt.Fprintf(&b, "created_at: %s\n", p.CreatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "updated_at: %s\n", p.UpdatedAt.UTC().Format(time.RFC3339))
	b.WriteString("---\n")
	fmt.Fprintf(&b, "# %s\n", firstLine(p.Description))
	b.WriteString("## Pattern\n")
	b.WriteString(p.Pattern)
	b.WriteString("\n## Examples\n")
	for _, ex := range p.Examples {
		fmt.Fprintf(&b, "- %s\n", ex)
	}
	return b.String()
}

// firstLine returns the text up to the first newline, trimmed.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// RebuildIndex regenerates index.md from the pattern pages currently on
// disk. One bullet per pattern:
//
//   - [slug](patterns/slug.md): <description first line>
//
// Pages with an empty description are listed with an empty one-liner after a
// WARN is logged (the paper's index wants PROBLEM+ROOT CAUSE+FIX; v1 renders
// the description).
func (w *WikiStore) RebuildIndex() error {
	patterns, err := w.LoadPatterns()
	if err != nil {
		return fmt.Errorf("wiki: rebuild index: %w", err)
	}

	// Sort for deterministic output across rebuilds.
	sort.Slice(patterns, func(i, j int) bool {
		a, b := patterns[i], patterns[j]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return patternSlug(a) < patternSlug(b)
	})

	var b strings.Builder
	for _, p := range patterns {
		slug := patternSlug(p)
		if strings.TrimSpace(p.Description) == "" {
			w.logger.Warn("wiki: pattern page has no description; index bullet empty",
				"slug", slug, "id", p.ID)
		}
		fmt.Fprintf(&b, "- [%s](patterns/%s.md): %s\n", slug, slug, firstLine(p.Description))
	}

	indexPath := filepath.Join(w.dir, wikiIndexFile)
	tmpPath := indexPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("wiki: write tmp index: %w", err)
	}
	if err := os.Rename(tmpPath, indexPath); err != nil {
		if rmErr := os.Remove(tmpPath); rmErr != nil {
			w.logger.Warn("wiki: remove tmp index failed", "path", tmpPath, "error", rmErr)
		}
		return fmt.Errorf("wiki: rename index: %w", err)
	}
	return nil
}

// AppendLog appends one free-text entry to the append-only logs.md as
// "<RFC3339> <entry>\n". Append-only files are intentionally never rewritten.
func (w *WikiStore) AppendLog(entry string) error {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return fmt.Errorf("wiki: create dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(w.dir, wikiLogFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("wiki: open log: %w", err)
	}
	defer f.Close() //nolint:errcheck // append-only file; read side reports state
	if _, err := fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339), entry); err != nil {
		return fmt.Errorf("wiki: append log: %w", err)
	}
	return nil
}

// AppendSkillImpact appends one JSONL row to the append-only skill-impact
// ledger.
func (w *WikiStore) AppendSkillImpact(e SkillImpactEntry) error {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return fmt.Errorf("wiki: create dir: %w", err)
	}
	row, err := jsonMarshalLine(e)
	if err != nil {
		return fmt.Errorf("wiki: marshal impact entry: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(w.dir, wikiImpactFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("wiki: open ledger: %w", err)
	}
	defer f.Close() //nolint:errcheck // append-only file
	if _, err := f.WriteString(row); err != nil {
		return fmt.Errorf("wiki: append ledger row: %w", err)
	}
	return nil
}

// ReadSkillImpact returns the full ledger content. A missing ledger is not an
// error and returns an empty string.
func (w *WikiStore) ReadSkillImpact() (string, error) {
	data, err := os.ReadFile(filepath.Join(w.dir, wikiImpactFile))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("wiki: read ledger: %w", err)
	}
	return string(data), nil
}

// ReadIndex returns the current index.md content. A missing index is not an
// error and returns an empty string.
func (w *WikiStore) ReadIndex() (string, error) {
	data, err := os.ReadFile(filepath.Join(w.dir, wikiIndexFile))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("wiki: read index: %w", err)
	}
	return string(data), nil
}

// LoadPatterns parses every patterns/*.md page back into a LearnedPattern.
// A page missing its ID is repaired with a freshly generated ID (with a
// warning); a page that cannot be parsed is skipped with a warning. Load
// errors (unreadable directory) are returned; per-page problems are not.
func (w *WikiStore) LoadPatterns() ([]*LearnedPattern, error) {
	patternsDir := filepath.Join(w.dir, wikiPatternsDir)
	entries, err := os.ReadDir(patternsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("wiki: read patterns dir: %w", err)
	}

	patterns := make([]*LearnedPattern, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(patternsDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			w.logger.Warn("wiki: unreadable pattern page skipped",
				"path", path, "error", err)
			continue
		}
		p := parsePatternPage(string(data))
		if p == nil {
			w.logger.Warn("wiki: corrupt pattern page skipped", "path", path)
			continue
		}
		if p.ID == "" {
			p.ID = id.Generate("pat-")
			w.logger.Warn("wiki: pattern page missing id; regenerated",
				"path", path, "new_id", p.ID)
		}
		// The frozen frontmatter carries no domain/content_hash fields, so
		// restore them from the canonical filename "<domain>-<hash12>.md".
		// This keeps slugs stable across load→upsert round-trips.
		if p.Domain == "" || p.ContentHash == "" {
			name := strings.TrimSuffix(entry.Name(), ".md")
			if i := strings.LastIndex(name, "-"); i > 0 {
				if p.Domain == "" {
					p.Domain = name[:i]
				}
				if p.ContentHash == "" {
					p.ContentHash = name[i+1:]
				}
			}
		}
		patterns = append(patterns, p)
	}
	return patterns, nil
}

// parsePatternPage parses the frozen page format rendered by
// renderPatternPage. It returns nil when the page has no frontmatter (the
// minimum structural requirement); unknown frontmatter keys and missing
// optional fields are tolerated.
func parsePatternPage(content string) *LearnedPattern {
	if !strings.HasPrefix(content, "---\n") {
		return nil
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return nil
	}
	fm := parseFrontmatterFields(rest[:end])
	body := rest[end+len("\n---\n"):]

	p := &LearnedPattern{}
	if v, ok := fm["id"]; ok {
		p.ID = strings.TrimSpace(v)
	}
	if v, ok := fm["type"]; ok {
		p.Type = PatternType(strings.TrimSpace(v))
	}
	if v, ok := fm["status"]; ok {
		p.Status = PatternStatus(strings.TrimSpace(v))
	}
	if v, ok := fm["confidence"]; ok {
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			p.Confidence = f
		}
	}
	if v, ok := fm["use_count"]; ok {
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			p.UseCount = n
		}
	}
	if v, ok := fm["created_at"]; ok {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(v)); err == nil {
			p.CreatedAt = t
		}
	}
	if v, ok := fm["updated_at"]; ok {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(v)); err == nil {
			p.UpdatedAt = t
		}
	}

	parsePatternBody(body, p)
	return p
}

// parsePatternBody fills the description/pattern/examples sections of p from
// the markdown body.
func parsePatternBody(body string, p *LearnedPattern) {
	// Body shape: "# <description>\n## Pattern\n<pattern>\n## Examples\n..."
	head, rest, hasPattern := strings.Cut(body, "## Pattern\n")
	p.Description = firstLine(strings.TrimPrefix(head, "# "))

	patternText := ""
	examplesText := ""
	if hasPattern {
		var hasEx bool
		patternText, examplesText, hasEx = strings.Cut(rest, "## Examples\n")
		if hasEx {
			// Examples section present: strip the leading newline it carries.
			examplesText = strings.TrimLeft(examplesText, "\n")
		}
		// hasEx false ⇒ no "## Examples" heading; examples stay empty (tolerated).
	} else {
		// Tolerate a missing Pattern section: everything after the heading is
		// treated as ... nothing; Examples may still follow directly.
		if _, after, ok := strings.Cut(body, "## Examples\n"); ok {
			examplesText = after
		}
	}

	p.Pattern = strings.TrimSpace(patternText)
	if examplesText != "" {
		for _, line := range strings.Split(strings.TrimRight(examplesText, "\n"), "\n") {
			line = strings.TrimSpace(line)
			if ex, ok := strings.CutPrefix(line, "- "); ok {
				p.Examples = append(p.Examples, ex)
			}
		}
	}
}

// parseFrontmatterFields parses "key: value" lines into a map. Values may
// contain ':'; keys are lowercased and trimmed.
func parseFrontmatterFields(fm string) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(fm, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return fields
}

// jsonMarshalLine marshals one JSONL row including its trailing newline.
func jsonMarshalLine(e SkillImpactEntry) (string, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
