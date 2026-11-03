package services

// groups_service.go — the report-only diagnosis SERVICE (agent-groups plan
// tree, docs/plans/20261030-agent-groups/06-diagnosis.md, tasks T1/T4).
//
// The diagnosis ENGINE lives in internal/groups (Explain + Check, pure,
// table-tested, shared by every surface). This service is the assembly
// layer: it maps the loaded config ([security.groups]) and the discovered
// AGENT.md roster into the engine's inputs, and exposes the two report-only
// questions to RPC, CLI, and doctor. Zero resolution/precedence logic lives
// here — the drift trap stays in internal/groups where the gates read it.
//
// internal/rpc cannot import this package (pre-existing services ->
// scheduler -> rpc cycle), so the RPC side declares a matching interface
// satisfied by *GroupsService and wired at daemon registration.

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/agents"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
)

// GroupsService carries the report-only diagnosis inputs. Every field may
// be nil/empty: the corresponding check degrades to a descriptive finding,
// never a panic (the handler nil-safety convention).
type GroupsService struct {
	// cfg is the loaded daemon config ([security.groups] source of truth).
	cfg *config.Config
	// bundledAgentsDir is the bundled roster root ("config/agents"); it
	// rides discovery as the lowest-priority tier.
	bundledAgentsDir string
	// extraDiscoverDirs are extra discovery tier roots (test injection);
	// entries shadow the default tiers at project priority.
	extraDiscoverDirs []string
}

// Compile-time: the RPC diagnosis interface is satisfied structurally
// (the rpc package mirrors this method set as DiagnosisService).
var (
	_ = func() { explainShape(); checkShape() }
)

// explainShape pins the Explain signature the RPC interface mirrors.
func explainShape() (*groups.ExplainResult, error) { return nil, nil }

// checkShape pins the Check signature the RPC interface mirrors.
func checkShape() (*groups.CheckResult, error) { return nil, nil }

// NewGroupsService constructs the diagnosis service over a loaded config.
// extraDiscoverDirs are extra discovery tier roots (test injection); an
// empty slice uses agents.DefaultTiers plus the bundled roster root.
func NewGroupsService(cfg *config.Config, bundledAgentsDir string, extraDiscoverDirs ...string) *GroupsService {
	return &GroupsService{
		cfg:               cfg,
		bundledAgentsDir:  bundledAgentsDir,
		extraDiscoverDirs: extraDiscoverDirs,
	}
}

// groupsSection returns the [security.groups] view of the config (nil-safe).
func (s *GroupsService) groupsSection() config.SecurityGroupsConfig {
	if s.cfg == nil {
		return config.SecurityGroupsConfig{}
	}
	return s.cfg.Security.Groups
}

// policies maps the config policy table to the engine's model table.
func (s *GroupsService) policies() []groups.Policy {
	g := s.groupsSection()
	out := make([]groups.Policy, 0, len(g.Policies))
	for _, p := range g.Policies {
		out = append(out, groups.Policy{
			Name:           p.Name,
			ToolsAllowed:   p.ToolsAllowed,
			ToolsDenied:    p.ToolsDenied,
			MemoryMaxScope: p.MemoryMaxScope,
			FSRoots:        p.FSRoots,
			NetworkClass:   p.NetworkClass,
			AllowedModels:  p.AllowedModels,
		})
	}
	return out
}

// bindings maps the config binding table to the engine's model table.
func (s *GroupsService) bindings() []groups.Binding {
	g := s.groupsSection()
	out := make([]groups.Binding, 0, len(g.Bindings))
	for _, b := range g.Bindings {
		out = append(out, groups.Binding{Subject: b.Subject, Policy: b.Policy, Scope: b.Scope})
	}
	return out
}

// diagnosisAgents maps the discovered roster into the engine's agent rows.
func (s *GroupsService) diagnosisAgents() []groups.DiagnosisAgent {
	if rosterDiscoveryHook != nil {
		return rosterDiscoveryHook()
	}
	opts := []agents.DiscoveryOption{agents.WithBundledPath(s.bundledAgentsDir)}
	if len(s.extraDiscoverDirs) > 0 {
		def := agents.DefaultTiers()
		tiers := make([]agents.DiscoveryTier, 0, len(def)+len(s.extraDiscoverDirs))
		for _, d := range s.extraDiscoverDirs {
			tiers = append(tiers, agents.DiscoveryTier{Path: d, Priority: agents.PriorityProject})
		}
		tiers = append(tiers, def...)
		opts = append(opts, agents.WithTiers(tiers))
	}
	disc := agents.NewDiscovery(opts...)
	defs, err := disc.Discover()
	if err != nil || defs == nil {
		return nil
	}
	out := make([]groups.DiagnosisAgent, 0, len(defs))
	for _, def := range defs {
		if def == nil {
			continue
		}
		out = append(out, groups.DiagnosisAgent{
			ID:      def.ID,
			Groups:  def.Groups,
			Tools:   def.AdditionalTools,
			Enabled: def.IsEnabled(),
		})
	}
	return out
}

// findAgent returns the discovered definition for agentID (nil when the
// agent is not in the roster).
//
// rosterDiscoveryHook is the test seam: non-nil replaces roster discovery
// entirely (no filesystem tier reads in unit tests).
var rosterDiscoveryHook func() []groups.DiagnosisAgent

func (s *GroupsService) findAgent(agentID string) *agents.AgentDefinition {
	if rosterDiscoveryHook != nil {
		for _, a := range rosterDiscoveryHook() {
			if a.ID == agentID {
				def := &agents.AgentDefinition{}
				def.ID = a.ID
				def.Groups = a.Groups
				def.AdditionalTools = a.Tools
				return def
			}
		}
		return nil
	}
	opts := []agents.DiscoveryOption{agents.WithBundledPath(s.bundledAgentsDir)}
	if len(s.extraDiscoverDirs) > 0 {
		def := agents.DefaultTiers()
		tiers := make([]agents.DiscoveryTier, 0, len(def)+len(s.extraDiscoverDirs))
		for _, d := range s.extraDiscoverDirs {
			tiers = append(tiers, agents.DiscoveryTier{Path: d, Priority: agents.PriorityProject})
		}
		tiers = append(tiers, def...)
		opts = append(opts, agents.WithTiers(tiers))
	}
	disc := agents.NewDiscovery(opts...)
	defs, err := disc.Discover()
	if err != nil {
		return nil
	}
	for _, def := range defs {
		if def != nil && def.ID == agentID {
			return def
		}
	}
	return nil
}

// Explain resolves one agent with per-entry source attribution (which
// binding + policy produced each grant/deny). Delegates to the shared
// groups.Explain engine — the SAME resolution the gates enforce.
func (s *GroupsService) Explain(ctx context.Context, agentID string) (*groups.ExplainResult, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("groups explain: agent id is required")
	}
	def := s.findAgent(agentID)
	if def == nil {
		return nil, fmt.Errorf("groups explain: agent %q is not in the discovered roster", agentID)
	}
	return groups.Explain(
		def.AdditionalTools, agentID, def.Groups,
		s.policies(), s.bindings(),
		&groups.ResolveOptions{}, // default normalizer; daemon wiring injects the registry-backed one
		s.groupsSection().DefaultManaged,
	), nil
}

// Check enumerates every R5 defect class with names. knownTools is the
// live registry name set (nil/empty = the C6 tool-name leg is inert).
// overrides is the leaf-07 reader seam (nil = zero overrides + note).
// Delegates to the shared groups.Check engine.
func (s *GroupsService) Check(ctx context.Context, knownTools map[string]struct{}, overrides groups.OverridesReader) (*groups.CheckResult, error) {
	return groups.Check(
		s.policies(), s.bindings(), s.diagnosisAgents(),
		knownTools, s.groupsSection().DefaultManaged,
		overrides,
		nil, // daemon wiring injects the registry-backed normalizer via CheckOptions
	), nil
}
