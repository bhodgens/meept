package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
)

// lanesArtifact is the frozen JSON shape the prompt-router sidecar consumes
// (ROUTER_LANES_FILE). Keep the field names stable: the sidecar parses them.
type lanesArtifact struct {
	Lanes []agent.LaneRoute `json:"lanes"`
	// Source is "frontmatter" when at least one lane came from an agent
	// definition's `intents:` list, "static" when the whole table came from
	// the built-in fallback. It is provenance for operators, not routing input.
	Source string `json:"source"`
}

// newLanesCmd prints the canonical lane -> agent routing table.
//
// The table is derived from the agent definitions (`intents:` frontmatter) with
// the built-in static table as the ordered fallback, so this command reports
// the same destinations the daemon classifier uses. Regenerate the artifact
// the prompt-router sidecar reads with:
//
//	meept lanes --json > $MEEPT_HOME/prompt_router_lanes.json
func newLanesCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "lanes",
		Short: "Print the intent routing table (lane to agent)",
		Long: "Print the canonical intent routing table.\n\n" +
			"Lanes and their destination agents come from the agent definitions under the\n" +
			"configured agent directories (`intents:` frontmatter); the built-in table is\n" +
			"the fallback for lanes no agent declares. This is the same table the daemon's\n" +
			"intent classifier routes with, so a new agent becomes routable by adding an\n" +
			"AGENT.md that declares the lane.\n\n" +
			"--json emits the artifact the prompt-router sidecar reads. Regenerate it with:\n\n" +
			"  meept lanes --json > $MEEPT_HOME/prompt_router_lanes.json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			artifact := buildLanesArtifact()
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(artifact)
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			if _, err := fmt.Fprintln(w, "intent\tagent"); err != nil {
				return err
			}
			for _, route := range artifact.Lanes {
				if _, err := fmt.Fprintf(w, "%s\t%s\n", route.Intent, route.Agent); err != nil {
					return err
				}
			}
			if err := w.Flush(); err != nil {
				return err
			}
			_, err := fmt.Fprintf(out, "\nlanes: %d (source: %s)\n", len(artifact.Lanes), artifact.Source)
			return err
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the routing artifact as JSON")
	return cmd
}

// buildLanesArtifact assembles the routing table from the agent definition
// directories in the same precedence the daemon uses (later directories
// override earlier ones), installs it, then reads the table back through the
// same resolver the classifier uses. Reading it back rather than formatting
// the index directly is the point: the printed table cannot drift from the
// routing the daemon performs.
func buildLanesArtifact() lanesArtifact {
	declared := map[string]string{}
	for _, dir := range agentConfigDirs() {
		index, err := agent.BuildLaneAgentIndexFromDir(dir)
		if err != nil {
			// A directory that does not exist is normal (a fresh install has
			// no ~/.meept/agents); a definition parse error means the agent
			// will not load in the daemon either, so skipping is honest here.
			continue
		}
		for lane, agentID := range index {
			declared[lane] = agentID
		}
	}
	source := "static"
	if len(declared) > 0 {
		agent.PublishLaneAgentIndex(declared)
		source = "frontmatter"
	}
	return lanesArtifact{Lanes: agent.LaneAgentTable(), Source: source}
}

// agentConfigDirs returns the configured agent definition directories in
// precedence order (later wins), with MEEPT_HOME honored. Falls back to the
// shipped defaults when the config cannot be read, so the command still works
// from a checkout that has never been configured.
func agentConfigDirs() []string {
	dirs := []string{"~/.meept/agents", "config/agents"}
	if cfg, err := config.LoadDefault(); err == nil && len(cfg.Agents.ConfigDirs) > 0 {
		dirs = cfg.Agents.ConfigDirs
	}
	resolved := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		resolved = append(resolved, config.ExpandMeeptPath(dir))
	}
	return resolved
}
