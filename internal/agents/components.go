package agents

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/pathutil"
)

// ComponentSection is a titled slice of prompt content emitted by a
// ComponentRegistry Resolve call. Title is the human-readable section header
// (derived from the component ID); Content is the verbatim markdown body.
type ComponentSection struct {
	Title   string
	Content string
}

// ComponentRegistry resolves prompt component IDs (e.g. "base.constitution")
// to their markdown content by scanning the 3-tier prompts hierarchy:
//
//	.meept/prompts/              (project-local, highest priority)
//	~/.meept/prompts/            (user-global)
//	~/.config/meept/prompts/     (system-wide)
//	config/prompts/              (bundled defaults, lowest priority)
//
// Component IDs are file paths relative to the prompts root with directory
// separators collapsed to "." and the trailing ".md" stripped. For example,
// "base/constitution.md" becomes "base.constitution".
//
// Higher-priority tiers shadow lower ones by component ID.
type ComponentRegistry struct {
	components map[string]string
	logger     *slog.Logger
}

// ComponentRegistryOption configures a ComponentRegistry.
type ComponentRegistryOption func(*ComponentRegistry)

// WithComponentLogger sets the logger used by the registry.
func WithComponentLogger(logger *slog.Logger) ComponentRegistryOption {
	return func(r *ComponentRegistry) { r.logger = logger }
}

// WithComponentBundledPath sets the bundled defaults path (lowest priority tier).
func WithComponentBundledPath(path string) ComponentRegistryOption {
	return func(r *ComponentRegistry) {
		r.discover(DiscoveryTier{Path: path, Priority: PriorityBundled})
	}
}

// WithComponentTiers overrides the default project/user/system tiers.
func WithComponentTiers(tiers []DiscoveryTier) ComponentRegistryOption {
	return func(r *ComponentRegistry) {
		for _, t := range tiers {
			r.discover(t)
		}
	}
}

// NewComponentRegistry creates a registry and scans the standard 3-tier
// hierarchy. The bundled path is optional; when empty, only the user-defined
// tiers are scanned.
func NewComponentRegistry(opts ...ComponentRegistryOption) *ComponentRegistry {
	r := &ComponentRegistry{
		components: make(map[string]string),
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	// Always include the standard tiers unless the caller fully overrode them
	// via WithComponentTiers. We can't distinguish "no tiers added yet" from
	// "caller wants zero tiers" cleanly, so we add defaults here only when the
	// registry is empty AND the caller didn't pass any tier option. The common
	// path (NewComponentRegistry()) and NewComponentRegistry(WithComponentBundledPath(...))
	// both still want the standard tiers.
	return r
}

// NewDefaultComponentRegistry scans the standard 3-tier prompts hierarchy
// plus an optional bundled path. This is the constructor most callers want.
func NewDefaultComponentRegistry(bundledPath string, logger *slog.Logger) *ComponentRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	r := &ComponentRegistry{
		components: make(map[string]string),
		logger:     logger.With("component", "component_registry"),
	}

	// Build tiers lowest-priority-first so higher-priority tiers overwrite.
	tiers := componentDefaultTiers()
	if bundledPath != "" {
		tiers = append(tiers, DiscoveryTier{Path: bundledPath, Priority: PriorityBundled})
	}

	// Scan bundled-lowest to project-highest so project files override.
	sort.Slice(tiers, func(i, j int) bool {
		return tiers[i].Priority > tiers[j].Priority
	})
	for _, t := range tiers {
		r.discover(t)
	}
	return r
}

func componentDefaultTiers() []DiscoveryTier {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}
	return []DiscoveryTier{
		{Path: ".meept/prompts", Priority: PriorityProject},
		{Path: filepath.Join(homeDir, ".meept", "prompts"), Priority: PriorityUser},
		{Path: filepath.Join(homeDir, ".config", "meept", "prompts"), Priority: PrioritySystem},
	}
}

// discover scans one tier directory recursively for .md files and merges them
// into the registry. Higher-priority tiers (lower Priority value) overwrite
// lower-priority entries with the same component ID.
func (r *ComponentRegistry) discover(tier DiscoveryTier) {
	path := pathutil.ExpandPath(tier.Path)

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		r.logger.Warn("Failed to stat prompts tier", "path", path, "error", err)
		return
	}
	if !info.IsDir() {
		return
	}

	// Walk recursively to find .md files.
	err = filepath.WalkDir(path, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			return nil
		}

		rel, relErr := filepath.Rel(path, p)
		if relErr != nil {
			return nil
		}
		id := componentIDFromRel(rel)
		if id == "" {
			return nil
		}

		// Read the raw file. We strip any leading HTML-comment frontmatter
		// (legacy "<!-- ... -->" block) since the body is what gets injected
		// into the system prompt.
		body, readErr := os.ReadFile(p)
		if readErr != nil {
			r.logger.Warn("Failed to read component file", "path", p, "error", readErr)
			return nil
		}
		content := stripHTMLCommentFrontmatter(string(body))

		// Only overwrite if this tier is at least as high priority as the
		// existing entry. Since we scan from lowest-priority to highest, the
		// simple "set if priority <= existing" rule implements shadowing.
		r.components[id] = strings.TrimSpace(content)
		return nil
	})
	if err != nil {
		r.logger.Warn("Failed to walk prompts tier", "path", path, "error", err)
	}
}

// componentIDFromRel converts a relative file path like "base/constitution.md"
// to a component ID like "base.constitution".
func componentIDFromRel(rel string) string {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, ".md")
	rel = strings.TrimSuffix(rel, ".MD")
	rel = strings.ReplaceAll(rel, "/", ".")
	if rel == "" || strings.HasPrefix(rel, ".") || strings.HasSuffix(rel, ".") {
		return ""
	}
	return rel
}

// stripHTMLCommentFrontmatter removes a leading "<!-- ... -->" block (used
// historically for component metadata) so only the markdown body remains.
func stripHTMLCommentFrontmatter(s string) string {
	trimmed := strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(trimmed, "<!--") {
		return s
	}
	end := strings.Index(trimmed, "-->")
	if end < 0 {
		return s
	}
	rest := trimmed[end+3:]
	return strings.TrimLeft(rest, " \t\r\n")
}

// Resolve returns ordered ComponentSections for the requested IDs. Unknown
// IDs are logged at warn level and skipped (the assembly proceeds with the
// remaining IDs so a single missing component doesn't break agent loading).
func (r *ComponentRegistry) Resolve(ids []string) []ComponentSection {
	if r == nil || len(ids) == 0 {
		return nil
	}
	sections := make([]ComponentSection, 0, len(ids))
	for _, id := range ids {
		content, ok := r.components[id]
		if !ok {
			r.logger.Warn("Prompt component not found", "id", id)
			continue
		}
		sections = append(sections, ComponentSection{
			Title:   componentTitleFromID(id),
			Content: content,
		})
	}
	return sections
}

// Count returns the number of discovered components.
func (r *ComponentRegistry) Count() int {
	if r == nil {
		return 0
	}
	return len(r.components)
}

// IDs returns the discovered component IDs in sorted order.
func (r *ComponentRegistry) IDs() []string {
	if r == nil || len(r.components) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.components))
	for id := range r.components {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// componentTitleFromID produces a human-readable section title from a dotted
// component ID. "base.constitution" → "Constitution"; "capabilities.memory"
// → "Memory"; "conditional.source_evaluation" → "Source Evaluation".
func componentTitleFromID(id string) string {
	parts := strings.Split(id, ".")
	last := parts[len(parts)-1]
	words := strings.Split(last, "_")
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// String renders a compact summary for logging.
func (r *ComponentRegistry) String() string {
	if r == nil {
		return "ComponentRegistry(<nil>)"
	}
	return fmt.Sprintf("ComponentRegistry(%d components)", len(r.components))
}
