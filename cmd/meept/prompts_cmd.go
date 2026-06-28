package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"text/template"

	"github.com/spf13/cobra"
)

func newPromptsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "prompts",
		Short:   "manage prompt templates",
		Long:    "list, inspect, edit, and validate the markdown prompt templates used by the planner, orchestrator, and reflection subsystems.",
		Aliases: []string{"prompt"},
	}
	cmd.AddCommand(newPromptsListCmd())
	cmd.AddCommand(newPromptsShowCmd())
	cmd.AddCommand(newPromptsEditCmd())
	cmd.AddCommand(newPromptsValidateCmd())
	return cmd
}

// promptServiceForCLI constructs a local PromptService for CLI use. The CLI
// operates directly on the 4-tier hierarchy without requiring a daemon
// connection — templates are static files on disk.
func promptServiceForCLI() *localPromptService {
	return &localPromptService{base: newLocalPromptBase()}
}

// localPromptService is a thin CLI-local wrapper that avoids importing the
// services package directly (keeps the CLI binary lean). It mirrors the
// 4-tier discovery order.
type localPromptService struct {
	base *localPromptBase
}

type localPromptBase struct {
	tiers []localTier
}

type localTier struct {
	label string
	dir   string
}

func newLocalPromptBase() *localPromptBase {
	home, _ := os.UserHomeDir()
	return &localPromptBase{
		tiers: []localTier{
			{"project", ".meept/prompts"},
			{"user", filepath.Join(home, ".meept", "prompts")},
			{"system", filepath.Join(home, ".config", "meept", "prompts")},
			{"bundled", "config/prompts"},
		},
	}
}

// userOverridePath returns the path in the user-home tier.
func (b *localPromptBase) userOverridePath(name string) string {
	if len(b.tiers) < 2 {
		return name
	}
	return filepath.Join(b.tiers[1].dir, name)
}

// findFirst walks tiers in priority order and returns the first match.
func (b *localPromptBase) findFirst(name string) (tier string, fullPath string, content []byte, ok bool) {
	name = normalizePromptName(name)
	for _, t := range b.tiers {
		full := filepath.Join(t.dir, name)
		body, err := os.ReadFile(full)
		if err == nil {
			return t.label, full, body, true
		}
	}
	return "", "", nil, false
}

func newPromptsListCmd() *cobra.Command {
	var outputJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list all prompt templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			b := newLocalPromptBase()
			entries := b.listAll()
			if outputJSON {
				out, _ := json.MarshalIndent(entries, "", "  ")
				fmt.Println(string(out))
				return nil
			}
			if len(entries) == 0 {
				fmt.Println("no prompt templates found")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tTIER\tSOURCE")
			for _, e := range entries {
				fmt.Fprintf(w, "%s\t%s\t%s\n", e["name"], e["tier"], e["source"])
			}
			w.Flush()
			fmt.Printf("\ntotal: %d templates\n", len(entries))
			return nil
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}

// listAll walks all tiers and returns de-duplicated entries.
func (b *localPromptBase) listAll() []map[string]string {
	seen := make(map[string]map[string]string)
	for _, tier := range b.tiers {
		var files []string
		_ = filepath.Walk(tier.dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(path), ".md") {
				return nil
			}
			rel, _ := filepath.Rel(tier.dir, path)
			rel = filepath.ToSlash(rel)
			files = append(files, rel)
			return nil
		})
		for _, rel := range files {
			if _, ok := seen[rel]; ok {
				continue
			}
			full := filepath.Join(tier.dir, rel)
			seen[rel] = map[string]string{
				"name":   rel,
				"tier":   tier.label,
				"source": full,
			}
		}
	}
	// Sort by name.
	result := make([]map[string]string, 0, len(seen))
	for _, v := range seen {
		result = append(result, v)
	}
	// Simple insertion sort by name (small N).
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j-1]["name"] > result[j]["name"]; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	return result
}

func newPromptsShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "show a prompt template's content",
		Long: `Show the content of a prompt template.

Name can be a full relative path (e.g. "planner/interview.md") or a bare
shorthand (e.g. "interview" expands to "planner/interview.md"). The first
match in the 4-tier hierarchy is shown.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b := newLocalPromptBase()
			tier, full, content, ok := b.findFirst(args[0])
			if !ok {
				return fmt.Errorf("template %q not found in any tier", args[0])
			}
			fmt.Printf("# source: %s (tier: %s)\n", full, tier)
			fmt.Print(string(content))
			if len(content) > 0 && content[len(content)-1] != '\n' {
				fmt.Println()
			}
			return nil
		},
	}
	return cmd
}

func newPromptsEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <name>",
		Short: "edit a prompt template in $EDITOR",
		Long: `Open the user-local override path for a template in $EDITOR
(fallback: vi). If the template only exists as a bundled file, it is copied
to ~/.meept/prompts/<path> first so you edit an override.

After the editor exits, the file is validated (non-empty + parses as
text/template).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := normalizePromptName(args[0])
			b := newLocalPromptBase()
			dest := b.userOverridePath(name)

			// If no user override exists, seed it from the first tier that
			// has the file (or create an empty file if truly new).
			if _, err := os.Stat(dest); os.IsNotExist(err) {
				_, _, content, ok := b.findFirst(name)
				if !ok {
					// New template — create with frontmatter stub.
					stub := fmt.Sprintf("---\nname: %s\ndescription: \n---\n", strings.TrimSuffix(filepath.Base(name), ".md"))
					content = []byte(stub)
				}
				if mkErr := os.MkdirAll(filepath.Dir(dest), 0o755); mkErr != nil {
					return fmt.Errorf("create override dir: %w", mkErr)
				}
				if wErr := os.WriteFile(dest, content, 0o644); wErr != nil {
					return fmt.Errorf("seed override: %w", wErr)
				}
			}

			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			ec := exec.Command(editor, dest)
			ec.Stdin = os.Stdin
			ec.Stdout = os.Stdout
			ec.Stderr = os.Stderr
			if err := ec.Run(); err != nil {
				return fmt.Errorf("editor exited with error: %w", err)
			}

			// Post-edit validation.
			edited, err := os.ReadFile(dest)
			if err != nil {
				return fmt.Errorf("read edited file: %w", err)
			}
			if len(strings.TrimSpace(string(edited))) == 0 {
				return fmt.Errorf("warning: edited file is empty")
			}
			if err := validateTemplateContent(string(edited)); err != nil {
				return fmt.Errorf("warning: edited file has template parse error: %w", err)
			}
			fmt.Printf("edited: %s\n", dest)
			return nil
		},
	}
	return cmd
}

func newPromptsValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate [name]",
		Short: "validate prompt templates",
		Long: `Validate that prompt templates parse as valid text/template.

Without a name argument, validates ALL discoverable templates.
With a name, validates only that one template.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b := newLocalPromptBase()
			if len(args) == 0 {
				entries := b.listAll()
				if len(entries) == 0 {
					fmt.Println("no templates found")
					return nil
				}
				failures := 0
				for _, e := range entries {
					content, err := os.ReadFile(e["source"])
					if err != nil {
						fmt.Printf("FAIL  %s  (read error: %v)\n", e["name"], err)
						failures++
						continue
					}
					if err := validateTemplateContent(string(content)); err != nil {
						fmt.Printf("FAIL  %s  %v\n", e["name"], err)
						failures++
						continue
					}
					fmt.Printf("ok    %s\n", e["name"])
				}
				if failures > 0 {
					return fmt.Errorf("%d/%d templates failed validation", failures, len(entries))
				}
				fmt.Printf("\nall %d templates valid\n", len(entries))
				return nil
			}
			// Validate single
			_, full, content, ok := b.findFirst(args[0])
			if !ok {
				return fmt.Errorf("template %q not found", args[0])
			}
			if err := validateTemplateContent(string(content)); err != nil {
				return fmt.Errorf("%s: %w", full, err)
			}
			fmt.Printf("ok: %s (%s)\n", normalizePromptName(args[0]), full)
			return nil
		},
	}
	return cmd
}

// normalizePromptName converts shorthand to full relative path. Mirrors
// services.normalizeName.
func normalizePromptName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "/")
	if !strings.Contains(name, "/") {
		base := strings.TrimSuffix(name, ".md")
		return "planner/" + base + ".md"
	}
	if !strings.HasSuffix(strings.ToLower(name), ".md") {
		name += ".md"
	}
	return name
}

// validateTemplateContent checks non-empty + parses as text/template after
// frontmatter stripping. Mirrors services.ValidateTemplate without the
// import dependency.
func validateTemplateContent(content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("template is empty")
	}
	body := stripFrontmatterCLI(content)
	if _, err := template.New("validate").Parse(body); err != nil {
		return fmt.Errorf("template parse error: %w", err)
	}
	return nil
}

// stripFrontmatterCLI removes a leading YAML frontmatter block. Mirrors the
// runtime behavior of agent.stripYAMLFrontmatter / services.stripFrontmatter.
func stripFrontmatterCLI(body string) string {
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
	if strings.HasSuffix(rest, "\n"+marker) {
		return ""
	}
	if strings.HasSuffix(rest, "\r\n"+marker) {
		return ""
	}
	return body
}
