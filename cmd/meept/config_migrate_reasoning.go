package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/caimlas/meept/internal/agents"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/pathutil"
)

// roleHeuristic maps agent ID -> suggested reasoning effort tier.
// Based on spec §10.4 role-based defaults. Agents not in this map (e.g.
// dispatcher, reviewers, scheduler) are skipped — the tool only suggests
// blocks for agents with a clear role-effort mapping.
var roleHeuristic = map[string]string{
	"planner":    llm.ReasoningXHigh,
	"debugger":   llm.ReasoningHigh,
	"analyst":    llm.ReasoningHigh,
	"researcher": llm.ReasoningHigh,
	"coder":      llm.ReasoningMedium,
	"committer":  llm.ReasoningLow,
	"chat":       llm.ReasoningLow,
}

// newConfigMigrateReasoningCmd creates the `meept config migrate-reasoning`
// subcommand. The command walks AGENT.md files across the standard 3-tier
// discovery hierarchy (plus bundled config/agents/) and suggests reasoning
// blocks based on the agent role heuristic. The user confirms each write
// unless --dry-run is set (print only). The --force flag allows overwriting
// agents that already have a reasoning block.
func newConfigMigrateReasoningCmd() *cobra.Command {
	var (
		dryRun  bool
		force   bool
		agentID string
	)
	cmd := &cobra.Command{
		Use:   "migrate-reasoning",
		Short: "suggest reasoning blocks based on agent role",
		Long: `Walk AGENT.md files and suggest per-agent reasoning effort blocks
based on role heuristics (planner -> xhigh, debugger -> high, coder -> medium, ...).

Examples:
  meept config migrate-reasoning --dry-run
  meept config migrate-reasoning
  meept config migrate-reasoning --agent coder
  meept config migrate-reasoning --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var filter []string
			if agentID != "" {
				filter = []string{agentID}
			}
			return runConfigMigrateReasoning(dryRun, force, filter)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print suggestions without writing")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing reasoning blocks (still prompts unless --dry-run)")
	cmd.Flags().StringVar(&agentID, "agent", "", "migrate only the named agent (default: all)")
	return cmd
}

// runConfigMigrateReasoning is the command entry point. It discovers AGENT.md
// files, applies the role heuristic, and either prints suggestions (dry-run)
// or prompts the user to apply each one. Returns nil if zero agents were
// found — callers should print a contextual message.
func runConfigMigrateReasoning(dryRun, force bool, agentIDs []string) error {
	files, err := discoverAgentFiles()
	if err != nil {
		return fmt.Errorf("discover agent files: %w", err)
	}
	if len(files) == 0 {
		fmt.Println("no AGENT.md files found in ~/.meept/agents, .meept/agents, or config/agents/")
		fmt.Println("nothing to migrate")
		return nil
	}

	filter := make(map[string]struct{}, len(agentIDs))
	for _, id := range agentIDs {
		filter[strings.ToLower(id)] = struct{}{}
	}

	type suggestion struct {
		path        string
		agentID     string
		effort      string
		existing    bool
		newYAML     string
		fullContent string
	}

	var suggestions []suggestion
	skipped := 0
	for _, path := range files {
		def, err := agents.ParseAgentFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to parse %s: %v\n", path, err)
			continue
		}
		id := strings.ToLower(def.ID)

		if len(filter) > 0 {
			if _, ok := filter[id]; !ok {
				continue
			}
		}

		effort, hasHeuristic := roleHeuristic[id]
		if !hasHeuristic {
			// No heuristic for this role (dispatcher, reviewers, scheduler).
			// Don't suggest.
			continue
		}

		existing := def.Reasoning != nil && def.Reasoning.Effort != ""
		if existing && !force {
			skipped++
			continue
		}

		newYAML := renderReasoningBlock(effort)
		newContent, err := injectReasoningBlock(path, effort)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to render for %s: %v\n", path, err)
			continue
		}

		suggestions = append(suggestions, suggestion{
			path:        path,
			agentID:     id,
			effort:      effort,
			existing:    existing,
			newYAML:     newYAML,
			fullContent: newContent,
		})
	}

	if len(suggestions) == 0 {
		if skipped > 0 {
			fmt.Printf("no suggestions; %d agent(s) already have a reasoning block (use --force to overwrite)\n", skipped)
		} else {
			fmt.Println("no agents with a role heuristic found; nothing to migrate")
		}
		return nil
	}

	if dryRun {
		fmt.Printf("dry-run: %d suggestion(s)", len(suggestions))
		if skipped > 0 {
			fmt.Printf(" (%d skipped — already have reasoning block)", skipped)
		}
		fmt.Println()
		fmt.Println()
		for _, s := range suggestions {
			fmt.Printf("agent: %s (%s)\n", s.agentID, s.path)
			fmt.Println("  suggested:")
			for _, line := range strings.Split(strings.TrimRight(s.newYAML, "\n"), "\n") {
				fmt.Printf("    %s\n", line)
			}
			if s.existing {
				fmt.Println("  note: overwrites existing reasoning block")
			}
			fmt.Println()
		}
		return nil
	}

	// Non-dry-run: prompt per-agent.
	reader := bufio.NewReader(os.Stdin)
	applied := 0
	for _, s := range suggestions {
		fmt.Printf("agent: %s (%s)\n", s.agentID, s.path)
		fmt.Println("  suggested:")
		for _, line := range strings.Split(strings.TrimRight(s.newYAML, "\n"), "\n") {
			fmt.Printf("    %s\n", line)
		}
		if s.existing {
			fmt.Println("  note: overwrites existing reasoning block")
		}
		fmt.Print("\napply suggested reasoning block? [y/N] ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		if line != "y" && line != "yes" {
			fmt.Println("  skipped")
			fmt.Println()
			continue
		}
		if err := atomicWriteFile(s.path, []byte(s.fullContent)); err != nil {
			fmt.Fprintf(os.Stderr, "  error: %v\n", err)
			fmt.Println()
			continue
		}
		fmt.Println("  applied")
		applied++
		fmt.Println()
	}

	fmt.Printf("done: %d/%d agent(s) updated\n", applied, len(suggestions))
	return nil
}

// discoverAgentFiles returns paths to all AGENT.md files across the 3-tier
// discovery hierarchy plus the bundled config/agents/ directory. Higher-
// priority tiers shadow lower ones for the same agent ID (so an override at
// ~/.meept/agents/coder/AGENT.md shadows config/agents/coder/AGENT.md).
// The returned slice is sorted by agent ID then by priority (highest first).
func discoverAgentFiles() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("user home dir: %w", err)
	}

	tiers := []struct {
		path string
		prio int
	}{
		{filepath.Join(home, ".meept", "agents"), 1},  // user-global
		{filepath.Join(home, ".config", "meept", "agents"), 2}, // system-wide
		{filepath.Join(".meept", "agents"), 0},         // project-local
		{filepath.Join("config", "agents"), 3},         // bundled
	}

	// Map id -> (path, prio) so lower-priority wins.
	byID := make(map[string]struct {
		path string
		prio int
	})

	for _, tier := range tiers {
		abs := pathutil.ExpandPath(tier.path)
		entries, err := os.ReadDir(abs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			agentFile := filepath.Join(abs, entry.Name(), "AGENT.md")
			info, err := os.Stat(agentFile)
			if err != nil || info.IsDir() {
				continue
			}
			id := strings.ToLower(entry.Name())
			existing, ok := byID[id]
			if !ok || tier.prio < existing.prio {
				byID[id] = struct {
					path string
					prio int
				}{agentFile, tier.prio}
			}
		}
	}

	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id].path)
	}
	return out, nil
}

// renderReasoningBlock returns the YAML text for a per-agent reasoning block
// at the given effort tier. The block is two-space indented to match the
// constraints: subsection in AGENT.md frontmatter.
func renderReasoningBlock(effort string) string {
	var b strings.Builder
	b.WriteString("reasoning:\n")
	fmt.Fprintf(&b, "  effort: %s\n", effort)
	b.WriteString("  allow_self_modulation: true\n")
	return b.String()
}

// injectReasoningBlock reads the AGENT.md file at path, parses the YAML
// frontmatter into an ordered map, adds or replaces the "reasoning" key with
// a heuristic block for the given effort tier, and returns the new full text
// (frontmatter + body). The body and all other frontmatter fields are
// preserved verbatim. Comments in the frontmatter are not preserved — this
// matches existing behavior of ConfigService.SaveAgent and the RPC reasoning
// endpoints.
func injectReasoningBlock(path, effort string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	text := string(raw)

	frontmatter, body, err := splitAgentFrontmatter(text)
	if err != nil {
		return "", fmt.Errorf("split frontmatter in %s: %w", path, err)
	}

	// Parse into a generic map to preserve unknown fields.
	var node yaml.Node
	if err := yaml.Unmarshal([]byte(frontmatter), &node); err != nil {
		return "", fmt.Errorf("parse YAML frontmatter: %w", err)
	}

	// Re-marshal the parsed frontmatter, replacing/inserting the reasoning
	// block. We use yaml.Node so that field ordering is preserved on
	// best-effort basis.
	root, err := ensureMappingNode(&node)
	if err != nil {
		return "", err
	}

	// Build the reasoning block as a mapping node.
	trueNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"}
	reasoningNode := &yaml.Node{
		Kind: yaml.MappingNode,
		Content: []*yaml.Node{
			scalarNode("effort"), scalarNode(effort),
			scalarNode("allow_self_modulation"), trueNode,
		},
	}

	setMappingField(root, "reasoning", reasoningNode)

	var fmBuf strings.Builder
	enc := yaml.NewEncoder(&fmBuf)
	enc.SetIndent(2)
	if err := enc.Encode(root); err != nil {
		return "", fmt.Errorf("re-encode frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("close encoder: %w", err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(strings.TrimRight(fmBuf.String(), "\n"))
	b.WriteString("\n---\n\n")
	b.WriteString(strings.TrimLeft(body, "\n"))
	return b.String(), nil
}

// splitAgentFrontmatter splits an AGENT.md file into frontmatter text (YAML
// payload between the --- markers, markers excluded) and the body. Returns
// an error if no frontmatter markers are found.
func splitAgentFrontmatter(text string) (frontmatter, body string, err error) {
	trimmed := strings.TrimLeft(text, " \t\n\r")
	if !strings.HasPrefix(trimmed, "---") {
		return "", "", errors.New("no leading --- frontmatter marker")
	}
	after := trimmed[3:]
	// Skip the rest of the opening line.
	if i := strings.Index(after, "\n"); i >= 0 {
		after = after[i+1:]
	} else {
		return "", "", errors.New("malformed opening --- marker")
	}

	closeIdx := strings.Index(after, "\n---")
	if closeIdx < 0 {
		// Try EOF --- suffix.
		t := strings.TrimRight(after, " \t\n\r")
		if strings.HasSuffix(t, "---") {
			return strings.TrimRight(t[:len(t)-3], "\n"), "", nil
		}
		return "", "", errors.New("no closing --- frontmatter marker")
	}
	frontmatter = after[:closeIdx]
	rest := after[closeIdx+4:]
	if i := strings.Index(rest, "\n"); i >= 0 {
		body = rest[i+1:]
	} else {
		body = ""
	}
	return frontmatter, body, nil
}

// ensureMappingNode ensures the parsed YAML is a mapping (document[0] is a
// MappingNode). If the document is empty, returns a fresh mapping.
func ensureMappingNode(docNode *yaml.Node) (*yaml.Node, error) {
	if docNode.Kind == yaml.DocumentNode && len(docNode.Content) > 0 {
		m := docNode.Content[0]
		if m.Kind == 0 {
			// Empty document — treat as fresh mapping.
			m.Kind = yaml.MappingNode
			return docNode, nil
		}
		if m.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("frontmatter root is %s, not mapping", kindName(m.Kind))
		}
		return docNode, nil
	}
	// Build a DocumentNode wrapping a MappingNode.
	mapping := &yaml.Node{Kind: yaml.MappingNode}
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mapping}}, nil
}

// setMappingField inserts or replaces the field with the given key in the
// mapping node's top level. If the document node passed is a DocumentNode,
// the underlying mapping is modified in place. The value node replaces any
// existing one.
func setMappingField(docNode *yaml.Node, key string, value *yaml.Node) {
	var mapping *yaml.Node
	if docNode.Kind == yaml.DocumentNode && len(docNode.Content) > 0 {
		mapping = docNode.Content[0]
	} else {
		mapping = docNode
	}
	if mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := mapping.Content[i]
		if k.Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}

// scalarNode builds a string scalar yaml.Node.
func scalarNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
}

// kindName returns a human-readable name for a yaml.Node kind.
func kindName(k yaml.Kind) string {
	switch k {
	case yaml.ScalarNode:
		return "scalar"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.DocumentNode:
		return "document"
	default:
		return fmt.Sprintf("kind(%d)", k)
	}
}

// atomicWriteFile writes data to path via a temp file in the same directory
// and renames. The temp file is created with the same mode as the destination
// (or 0600 if the destination doesn't exist yet).
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode()
	}

	tmp, err := os.CreateTemp(dir, ".migrate-reasoning-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp file: %w", err)
	}
	return nil
}
