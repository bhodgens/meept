package services

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// PromptTier labels the discovery tier a template was found in.
type PromptTier string

const (
	// TierProject is the project-local tier (.meept/prompts/).
	TierProject PromptTier = "project"
	// TierUser is the user-home tier (~/.meept/prompts/).
	TierUser PromptTier = "user"
	// TierSystem is the XDG-style system tier (~/.config/meept/prompts/).
	TierSystem PromptTier = "system"
	// TierBundled is the lowest-priority tier shipped with the binary (config/prompts/).
	TierBundled PromptTier = "bundled"
)

// PromptEntry describes one discoverable template file.
type PromptEntry struct {
	Name       string     `json:"name"`        // relative path, e.g. "planner/interview.md"
	Tier       PromptTier `json:"tier"`        // discovery tier of the first match
	SourcePath string     `json:"source_path"` // absolute or working-dir-relative path
	Modified   time.Time  `json:"modified"`    // modification time of the source file
}

// PromptDetail describes one template with its raw content.
type PromptDetail struct {
	PromptEntry
	Content string `json:"content"`
}

// PromptService wraps the 4-tier prompts hierarchy for HTTP/RPC access.
// It mirrors the discovery order used by agent.plannerTemplateLoader so CLI,
// TUI, and HTTP surfaces agree with runtime resolution.
type PromptService struct {
	tiers []tierSpec
}

type tierSpec struct {
	label PromptTier
	dir   string
}

// PromptDirs holds the resolved directories for the four prompt tiers.
//
// The daemon resolves every path before constructing the service: the bundled
// directory from the shipped repo/executable layout (never the process CWD),
// the user directory from config.MeeptPath("prompts") (which honors
// $MEEPT_HOME), the system directory from the XDG-style user config dir, and
// the project directory from the active project. An empty field means the
// tier is absent and is dropped.
type PromptDirs struct {
	Project string // active project's .meept/prompts
	User    string // $MEEPT_HOME/prompts
	System  string // ~/.config/meept/prompts
	Bundled string // shipped config/prompts, daemon-resolved
}

// NewPromptServiceFromDirs constructs a PromptService from daemon-resolved
// tier directories. Prefer this over NewPromptService at daemon wiring sites
// so prompt discovery never depends on the process working directory.
func NewPromptServiceFromDirs(d PromptDirs) *PromptService {
	return NewPromptService(d.Project, d.User, d.System, d.Bundled)
}

// NewPromptService constructs a PromptService using the standard 4-tier
// hierarchy. Empty directories are dropped and directories that resolve to
// the same path are de-duplicated (see resolveTiers).
//
// Tier order (highest priority first):
//  1. projectDir (the active project's .meept/prompts)
//  2. userDir    ($MEEPT_HOME/prompts, default ~/.meept/prompts)
//  3. systemDir  (~/.config/meept/prompts)
//  4. bundledDir (shipped config/prompts, resolved by the daemon)
func NewPromptService(projectDir, userDir, systemDir, bundledDir string) *PromptService {
	return &PromptService{
		tiers: resolveTiers([]tierSpec{
			{TierProject, projectDir},
			{TierUser, userDir},
			{TierSystem, systemDir},
			{TierBundled, bundledDir},
		}),
	}
}

// NewDefaultPromptService constructs a PromptService whose user tier honors
// $MEEPT_HOME (config.MeeptPath("prompts")) and whose system tier is the
// XDG-style ~/.config/meept/prompts.
//
// The project and bundled tiers are left empty: only the daemon can resolve
// the shipped bundled directory (repo/executable layout) and the active
// project's directory. Daemon wiring should pass both through
// NewPromptServiceFromDirs; this constructor is the fallback for callers with
// no daemon context and never reads the process CWD.
func NewDefaultPromptService() *PromptService {
	system := ""
	if home, err := os.UserHomeDir(); err == nil {
		system = filepath.Join(home, ".config", "meept", "prompts")
	}
	return NewPromptService("", config.MeeptPath("prompts"), system, "")
}

// resolveTiers drops empty directories and de-duplicates directories that
// resolve to the same absolute path.
//
// Empty dirs are dropped so filepath.Join("", name) can never silently read
// from the process CWD. Collisions matter for labels: when the project tier
// and user tier resolve to the same directory (which happens when the project
// dir is derived from a CWD that is the meept home), a file there is a user
// override and must be labelled "user", not "project". Other duplicate tiers
// keep the highest-priority (first) occurrence.
func resolveTiers(in []tierSpec) []tierSpec {
	userDir := dirForTier(in, TierUser)
	userAbs := ""
	if strings.TrimSpace(userDir) != "" {
		userAbs = absOrClean(userDir)
	}

	out := make([]tierSpec, 0, len(in))
	for _, t := range in {
		if strings.TrimSpace(t.dir) == "" {
			continue
		}
		abs := absOrClean(t.dir)
		// A "project" tier that is really the user directory is a user tier:
		// keep the truthful label instead of shadowing it.
		if t.label == TierProject && userAbs != "" && abs == userAbs {
			continue
		}
		dup := false
		for _, existing := range out {
			if absOrClean(existing.dir) == abs {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		out = append(out, tierSpec{label: t.label, dir: t.dir})
	}
	return out
}

// dirForTier returns the directory configured for the given tier label, or ""
// when that tier is absent.
func dirForTier(tiers []tierSpec, label PromptTier) string {
	for _, t := range tiers {
		if t.label == label {
			return t.dir
		}
	}
	return ""
}

// absOrClean returns the absolute, cleaned form of p for path comparison.
// Falls back to filepath.Clean when the absolute path cannot be computed.
func absOrClean(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}

// UserOverridePath returns the path in the user-home tier where an override
// for the given template name should be written. This is used by edit/PUT
// handlers so users edit an override rather than a bundled file.
//
// The user tier is located by label, not by index: resolveTiers drops empty
// and duplicate directories, so its position in the tier list is not fixed.
func (s *PromptService) UserOverridePath(name string) string {
	dir := dirForTier(s.tiers, TierUser)
	if dir == "" {
		return name
	}
	return filepath.Join(dir, name)
}

// List walks every tier and returns one entry per unique template name,
// keeping the highest-priority match. Names are relative paths using forward
// slashes (e.g. "planner/interview.md") so they are URL-safe and portable.
func (s *PromptService) List() ([]PromptEntry, error) {
	seen := make(map[string]PromptEntry)
	for _, tier := range s.tiers {
		files, err := walkMarkdown(tier.dir)
		if err != nil {
			// Missing tier directories are normal (e.g., no user overrides).
			continue
		}
		for _, rel := range files {
			relSlash := filepath.ToSlash(rel)
			if _, ok := seen[relSlash]; ok {
				continue // higher-priority tier already recorded it
			}
			full := filepath.Join(tier.dir, rel)
			info, err := os.Stat(full)
			if err != nil {
				continue
			}
			seen[relSlash] = PromptEntry{
				Name:       relSlash,
				Tier:       tier.label,
				SourcePath: full,
				Modified:   info.ModTime(),
			}
		}
	}
	result := make([]PromptEntry, 0, len(seen))
	for _, e := range seen {
		result = append(result, e)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// Get retrieves one template, walking the tiers in priority order. Returns
// the raw file content (frontmatter NOT stripped — use Validate to test
// rendering).
func (s *PromptService) Get(name string) (PromptDetail, error) {
	name = normalizeName(name)
	for _, tier := range s.tiers {
		full := filepath.Join(tier.dir, name)
		body, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		info, _ := os.Stat(full)
		var mod time.Time
		if info != nil {
			mod = info.ModTime()
		}
		return PromptDetail{
			PromptEntry: PromptEntry{
				Name:       name,
				Tier:       tier.label,
				SourcePath: full,
				Modified:   mod,
			},
			Content: string(body),
		}, nil
	}
	return PromptDetail{}, fmt.Errorf("prompt %q not found in any tier", name)
}

// Put writes an override to the user-home tier. If the template only exists
// at a lower tier, the existing content (if any) is copied first so the
// user edits from a known-good starting point. The content is validated
// (non-empty + parses as text/template after frontmatter stripping) before
// writing.
func (s *PromptService) Put(name, content string) error {
	name = normalizeName(name)
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("content must not be empty")
	}
	if err := ValidateTemplate(content); err != nil {
		return fmt.Errorf("template validation failed: %w", err)
	}
	if dirForTier(s.tiers, TierUser) == "" {
		return fmt.Errorf("no user prompt tier configured; cannot store override")
	}
	dest := s.UserOverridePath(name)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("create override dir: %w", err)
	}
	if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write override: %w", err)
	}
	return nil
}

// Delete removes the user-home override for the given name. Lower-tier
// (bundled) templates remain untouched.
func (s *PromptService) Delete(name string) error {
	name = normalizeName(name)
	dest := s.UserOverridePath(name)
	if err := os.Remove(dest); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no user override for %q", name)
		}
		return fmt.Errorf("delete override: %w", err)
	}
	return nil
}

// ValidateAll walks every discoverable template and validates that each
// parses as a text/template after frontmatter stripping. Returns a list of
// errors keyed by template name; an empty slice means all templates parse.
func (s *PromptService) ValidateAll() []ValidationError {
	entries, err := s.List()
	if err != nil {
		return []ValidationError{{Name: "(list)", Err: err}}
	}
	var errs []ValidationError
	for _, e := range entries {
		detail, err := s.Get(e.Name)
		if err != nil {
			errs = append(errs, ValidationError{Name: e.Name, Err: err})
			continue
		}
		if err := ValidateTemplate(detail.Content); err != nil {
			errs = append(errs, ValidationError{Name: e.Name, Err: err})
		}
	}
	return errs
}

// ValidateOne validates a single template by name.
func (s *PromptService) ValidateOne(name string) error {
	detail, err := s.Get(name)
	if err != nil {
		return err
	}
	return ValidateTemplate(detail.Content)
}

// ValidationError pairs a template name with a parse error.
type ValidationError struct {
	Name string `json:"name"`
	Err  error  `json:"-"`
	Msg  string `json:"error"` // Err.Error(), pre-stringified for JSON
}

// ValidateTemplate checks that content is non-empty and parses as a valid
// text/template after YAML frontmatter is stripped. It does NOT execute the
// template (placeholders may reference runtime-only fields).
func ValidateTemplate(content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("template is empty")
	}
	body := stripFrontmatter(content)
	if _, err := template.New("validate").Parse(body); err != nil {
		return fmt.Errorf("template parse error: %w", err)
	}
	return nil
}

// normalizeName converts user shorthand into a slash-relative path. If the
// name has no extension and no directory component, it is treated as a
// planner template (e.g., "interview" → "planner/interview.md"). Missing
// .md extensions are appended.
func normalizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "/")
	// Shorthand: bare name without slash → assume planner/.
	if !strings.Contains(name, "/") {
		base := strings.TrimSuffix(name, ".md")
		return "planner/" + base + ".md"
	}
	// Ensure .md extension for path-qualified names.
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name += ".md"
	}
	return name
}

// walkMarkdown recursively lists *.md files under root, returning paths
// relative to root.
func walkMarkdown(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil // skip unreadable
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// stripFrontmatter removes a leading YAML frontmatter block ("---\n...\n---\n").
// Mirrors the runtime behavior of agent.stripYAMLFrontmatter.
func stripFrontmatter(body string) string {
	const marker = "---"
	if !strings.HasPrefix(body, marker+"\n") && !strings.HasPrefix(body, marker+"\r\n") {
		return body
	}
	rest := body[len(marker):]
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else {
		rest = rest[1:]
	}
	// Try all four line-ending combinations around the closing marker.
	searches := []string{
		"\n" + marker + "\n",
		"\r\n" + marker + "\r\n",
		"\n" + marker + "\r\n",
		"\r\n" + marker + "\n",
	}
	for _, s := range searches {
		if idx := strings.Index(rest, s); idx >= 0 {
			return rest[idx+len(s):]
		}
	}
	// Closing marker at EOF without trailing newline.
	if strings.HasSuffix(rest, "\n"+marker) {
		return ""
	}
	if strings.HasSuffix(rest, "\r\n"+marker) {
		return ""
	}
	return body
}
