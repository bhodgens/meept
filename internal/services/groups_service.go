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
	"sort"
	"strings"

	"github.com/caimlas/meept/internal/agent"
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
	// employeeExplain resolves an employee id when the discovered roster
	// misses. Nil keeps roster-only explain (tests and unwired daemons).
	// The daemon injects this; services must not import internal/employee
	// (services -> employee -> bot -> comm/http -> services).
	employeeExplain EmployeeExplainResolver
}

// Compile-time: the RPC diagnosis interface is satisfied structurally
// (the rpc package mirrors this method set as DiagnosisService).
var (
	_ = func() { _, _ = explainShape(); _, _ = checkShape() }
)

// explainShape pins the Explain signature the RPC interface mirrors.
func explainShape() (*groups.ExplainResult, error) { return nil, nil } //nolint:nilnil // signature-pin stub (rpc mirrors this method set; never invoked for a value)

// checkShape pins the Check signature the RPC interface mirrors.
func checkShape() (*groups.CheckResult, error) { return nil, nil } //nolint:nilnil // signature-pin stub (rpc mirrors this method set; never invoked for a value)

// EmployeeExplain is the employee row groups explain may print. It is not
// a roster agent: tools come from the constitution, not group policy.
type EmployeeExplain struct {
	ID                  string
	ConstitutionPresent bool
	ToolsAllowed        []string
	ToolsForbidden      []string
}

// EmployeeExplainResolver looks up an employee id. found=false means the
// id is not an employee (not an error). A store failure is err.
type EmployeeExplainResolver interface {
	ExplainEmployee(ctx context.Context, id string) (EmployeeExplain, bool, error)
}

// SetEmployeeExplainResolver injects the employee lookup. Nil is ignored
// (setter convention) so a missing manager cannot wipe a wired resolver.
func (s *GroupsService) SetEmployeeExplainResolver(r EmployeeExplainResolver) {
	if s == nil || r == nil {
		return
	}
	s.employeeExplain = r
}

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
			ID:     def.ID,
			Groups: def.Groups,
			// The diagnosis vocabulary must be the SAME grant set the
			// runtime gate resolves (registry.go ResolveForAgent passes
			// spec.GrantedToolNames()): baseline + additional, normalized
			// to registered names. Feeding only AdditionalTools made
			// diagnosis resolve a different (baseline-less) tool set than
			// enforcement — under default_managed an empty-additional
			// agent diagnosed to the full default-policy allowlist while
			// the gate intersected with its baseline grants.
			Tools:   diagnosisGrantSet(def.AdditionalTools),
			Enabled: def.IsEnabled(),
		})
	}
	return out
}

// diagnosisGrantSet builds the full normalized grant vocabulary for a
// discovered definition: agents.AgentDefinition carries only the declared
// additional tools, so the baseline half comes from the exported agent
// vocabulary and the union rides the SAME GrantedToolNames method the
// runtime gate uses (internal/agent/spec.go; registry.go ResolveForAgent).
// No parallel baseline list may live here — the runtime's BaselineTools is
// the single source of truth.
func diagnosisGrantSet(additionalTools []string) []string {
	spec := &agent.AgentSpec{AdditionalTools: additionalTools}
	return spec.GrantedToolNames()
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

// Employee explain markers. The CLI human renderer (cmd/meept/groups.go)
// keys off these exact strings. They are not roster subjects: an employee
// id must not be explained as a discovered AGENT.md agent.
const (
	employeeExplainSubjectPrefix       = "employee:"
	employeeExplainConstitutionPresent = "constitution present"
	employeeExplainConstitutionAbsent  = "constitution absent"
	employeeExplainToolsAllowed        = "tools_allowed"
	employeeExplainToolsForbidden      = "tools_forbidden"
)

// Explain resolves one agent with per-entry source attribution (which
// binding + policy produced each grant/deny). Delegates to the shared
// groups.Explain engine — the SAME resolution the gates enforce.
//
// A roster hit stays on that path and is byte-identical. A roster miss
// tries the injected employee resolver. An employee row is NOT passed
// through groups.Explain: that would pretend the employee is a roster
// agent. A miss on both still fails.
func (s *GroupsService) Explain(ctx context.Context, agentID string) (*groups.ExplainResult, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("groups explain: agent id is required")
	}
	def := s.findAgent(agentID)
	if def == nil {
		return s.explainEmployee(ctx, agentID)
	}
	return groups.Explain(
		diagnosisGrantSet(def.AdditionalTools), agentID, def.Groups,
		s.policies(), s.bindings(),
		&groups.ResolveOptions{}, // default normalizer; daemon wiring injects the registry-backed one
		s.groupsSection().DefaultManaged,
	), nil
}

// explainEmployee answers a roster miss from the employee row. A nil
// resolver, or a resolver that does not know the id, keeps the roster-miss
// error so a missing employee and a missing roster agent both fail.
func (s *GroupsService) explainEmployee(ctx context.Context, agentID string) (*groups.ExplainResult, error) {
	if s == nil || s.employeeExplain == nil {
		return nil, fmt.Errorf("groups explain: agent %q is not in the discovered roster", agentID)
	}
	view, found, err := s.employeeExplain.ExplainEmployee(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("groups explain: employee %q: %w", agentID, err)
	}
	if !found {
		return nil, fmt.Errorf("groups explain: agent %q is not in the discovered roster", agentID)
	}
	return employeeExplainResult(agentID, view), nil
}

// employeeExplainResult builds the explain answer from the employee row.
// AllowedTools and DeniedTools stay empty: those are roster resolution
// sets. Constitution tools ride grants whose reason is tools_allowed or
// tools_forbidden, attributed to subject employee:<id>.
func employeeExplainResult(requestedID string, view EmployeeExplain) *groups.ExplainResult {
	id := strings.TrimSpace(view.ID)
	if id == "" {
		id = requestedID
	}
	subject := employeeExplainSubjectPrefix + id
	note := employeeExplainConstitutionAbsent
	if view.ConstitutionPresent {
		note = employeeExplainConstitutionPresent
	}
	grants := make([]groups.GrantEntry, 0, len(view.ToolsAllowed)+len(view.ToolsForbidden))
	for _, tool := range view.ToolsAllowed {
		if tool == "" {
			continue
		}
		grants = append(grants, groups.GrantEntry{
			Tool:           tool,
			Effect:         "allow",
			BindingSubject: subject,
			Reason:         employeeExplainToolsAllowed,
		})
	}
	for _, tool := range view.ToolsForbidden {
		if tool == "" {
			continue
		}
		grants = append(grants, groups.GrantEntry{
			Tool:           tool,
			Effect:         "deny",
			BindingSubject: subject,
			Reason:         employeeExplainToolsForbidden,
		})
	}
	sort.Slice(grants, func(i, j int) bool {
		if grants[i].Effect != grants[j].Effect {
			return grants[i].Effect < grants[j].Effect
		}
		return grants[i].Tool < grants[j].Tool
	})
	return &groups.ExplainResult{
		AgentID: id,
		Bindings: []groups.ExplainSource{{
			Subject: subject,
			Note:    note,
		}},
		Grants: grants,
	}
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
