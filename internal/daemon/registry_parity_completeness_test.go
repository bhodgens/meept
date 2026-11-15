package daemon

// registry_parity_completeness_test.go — registry-parity completeness gate.
//
// Bug class (bughunt H7 / C32 / C33 lineage): a model-facing tool is
// implemented and registered into the daemon's live tools.Registry, but its
// row is missing from agent.ToolActionMap (internal/agent/executor.go:257)
// or its resolved action has no row in security.BuiltinRules
// (pkg/security/permissions.go:35). Executor.checkPermission falls back to
// the tool NAME as the action when the map lacks a row;
// PermissionChecker.CheckPermissionForAgent denies every action unknown to
// BuiltinRules ("Unknown action", fail-closed). Net effect: implemented +
// registered + granted, yet INERT at runtime — every model call is denied.
//
// internal/agent/tool_action_completeness_test.go covers the same class
// from the AGENT side with a hand-pinned name list. This gate covers it
// from the DAEMON side: the registered set is not a hand-kept list but the
// live registry produced by the real production wiring path
// (NewComponents + Start — the same seam driven by e.g.
// groups_boot_live_registry_test.go), plus every compiled-in toolprov
// provider walked hermetically. Providers the daemon arms only behind
// default-off config gates (github, goal, acp, hermes — github_wiring.go,
// dsh_wiring.go, acp_register.go, hermes_register.go) are bound here with
// the same Set*Provider constructors the daemon wiring sites call, so the
// gate sees the full provider universe, never a vacuous skip.
//
// Assertions (both directions):
//  1. FORWARD: every registered/produced tool name resolves to a known
//     base rule through ToolActionMap + the identity fallback + the
//     fail-closed prefix classifiers the checker consults
//     (cua-driver.* / browser_* observation, acp_agent). A name that does
//     not resolve is registered-but-inert — FAILS unless allowlisted with
//     an open finding id (see rp2InertAllowlist).
//  2. REVERSE: every ToolActionMap key is either produced by this boot or
//     pinned as reachable behind a documented default-off gate
//     (rp2GatedRegistrationPins). A key in neither set is a row for a tool
//     no gate can ever register — a renamed/removed tool left behind.
//  3. RULES: every BuiltinRules key is reached by the resolved action of
//     some registrable tool (live or gate-pinned), or is allowlisted as an
//     admin/manual-path action (rp2AdminRuleAllowlist).
//
// Hermetic: no network, no LLM, no runtime spawns, no subprocess
// execution (gates only arm registrations; subprocess work happens at
// Execute time, never here). Model files are absent so local runtimes log
// + skip exactly as every other daemon wiring test boots; stores live
// under t.TempDir().

import (
	"context"
	"sort"
	"testing"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/toolprov"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/internal/tools/builtin"
	"github.com/caimlas/meept/pkg/security"
)

// rp2StubChain satisfies builtin.ContactChainRunner (a function literal
// cannot: the seam is a named interface) — same posture as the stubChain in
// internal/agent/tool_action_completeness_test.go.
type rp2StubChain struct{}

func (rp2StubChain) Run(person, message string, channels []string) string { return "" }

// rp2StubGoal satisfies builtin.goalManager, the way the daemon's
// goalEmployeeManagerAdapter adapts *employee.Manager. The gate only needs
// Discover()'s names to resolve, so nil-returning methods are sufficient
// (goal tools fail closed at Execute against an empty store).
type rp2StubGoal struct{}

func (rp2StubGoal) CreateGoal(ctx context.Context, spec builtin.GoalCreateSpec) (*builtin.GoalRecord, error) {
	return nil, nil
}
func (rp2StubGoal) GetGoal(ctx context.Context, id string) (*builtin.GoalRecord, error) {
	return nil, nil
}
func (rp2StubGoal) ListActive(ctx context.Context, owner string) ([]*builtin.GoalRecord, error) {
	return nil, nil
}
func (rp2StubGoal) UpdateGoal(ctx context.Context, g *builtin.GoalRecord) error { return nil }

// rp2StaticToken is a githubsvc.TokenSource returning a static literal. The
// pool resolves the token at request time only: constructing the pool never
// authenticates and this gate never performs a GitHub request.
type rp2StaticToken struct{}

func (rp2StaticToken) Token(ctx context.Context) (string, error) { return "rp2-parity-token", nil }

// rp2BootParityUniverse boots the daemon's REAL registration path
// (NewComponents + Start) with the tool gates flipped on that only ADD
// registry entries (transcript_fetch, knowledge family, code-intelligence
// AST tools, codemode, timeout_probe, skills authoring), then binds + walks
// every compiled-in toolprov provider — including the three groups the
// default-off boot registers live (delegate, scheduler, voice) and the four
// behind default-off config gates (github, goal, acp, hermes). It returns
// the union: live registry canonical names plus every provider-discovered
// name. Providers that are bound but not armed fail the walk loudly — an
// unbound provider contributes zero tools and the gate would pass vacuously
// over its whole tool group (the posture the agent-side gate documents).
func rp2BootParityUniverse(t *testing.T) map[string]struct{} {
	t.Helper()
	t.Setenv("MEEPT_HOME", t.TempDir())
	dataDir := t.TempDir()
	cfg := &config.Config{
		Daemon:   config.DaemonConfig{DataDir: dataDir},
		Security: config.SecurityConfig{AllowedPaths: []string{dataDir}},
	}
	cfg.Transcript.Enabled = true
	cfg.Memory.Knowledge.Enabled = true
	cfg.CodeIntel.Enabled = true
	cfg.Agent.Tools.CodemodeEnabled = true
	cfg.Agent.ToolTimeoutProbeEnabled = true
	cfg.Skills.Enabled = true

	msgBus := bus.New(nil, testLogger(t))
	comps, err := NewComponents(context.Background(), cfg, msgBus, testLogger(t))
	if err != nil {
		t.Fatalf("NewComponents: %v", err)
	}
	t.Cleanup(func() { _ = comps.Stop(context.Background()) })
	if err := comps.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	universe := comps.ToolRegistry.CanonicalNames()

	t.Cleanup(builtin.ResetDelegateProviderForTest) // restore fail-closed default
	t.Cleanup(builtin.ResetGoalProviderForTest)
	t.Cleanup(builtin.ResetACPProviderForTest)

	// Bind the providers the daemon arms only behind runtime gates, with
	// the same Set*Provider constructors its wiring sites use (equivalence
	// basis: github_wiring.go's SetGitHubProvider(opts, ledger, sink,
	// broker) with a hermetic pool over a static token source;
	// dsh_wiring.go's SetGoalProvider(adapter, owner) with an
	// empty-store adapter; acp_register.go's SetACPProvider(mgr, broker)
	// with an enabled manager; hermes_register.go's SetHermesProvider with
	// the nil client the disabled-key posture arms; the scheduler/voice/
	// delegate binds mirror components.go's deferred wiring sites with nil
	// controllers / stub closures that tolerate nil primitives and fail
	// closed at Execute — this gate needs Discover()'s names only).
	builtin.SetSchedulerProvider(nil, nil)
	builtin.SetDelegateProvider(builtin.NewDelegateController(nil, nil, nil))
	builtin.SetVoiceProvider(
		func(context.Context, string, string, string) (string, error) { return "rp2", nil },
		func(context.Context) string { return "rp2-agent" }, nil)
	builtin.SetContactChain(rp2StubChain{})
	builtin.SetHermesProvider(nil, false, nil)
	pool, perr := githubsvc.NewClientPool(githubsvc.ClientPoolOptions{Tokens: rp2StaticToken{}})
	if perr != nil {
		t.Fatalf("github client pool construction: %v", perr)
	}
	builtin.SetGitHubProvider(githubsvc.RegistrationOptions{Pool: pool}, nil, nil, nil)
	builtin.SetGoalProvider(rp2StubGoal{}, func(ctx context.Context) string { return "rp2-parity" })
	builtin.SetACPProvider(acp.NewManager(config.ACPConfig{Enabled: true}, nil), nil)

	for _, pname := range toolprov.Names() {
		factory, ok := toolprov.Factory(pname)
		if !ok {
			t.Fatalf("toolprov provider %q not registered (known: %v) — the seam moved, update this gate", pname, toolprov.Names())
		}
		provider := factory()
		if provider == nil {
			t.Errorf("toolprov provider %q factory returned nil (unbound): the walk would pass vacuously over its whole tool group — bind it like the daemon wiring site does", pname)
			continue
		}
		produced := 0
		for _, tool := range provider.Discover() {
			if tool == nil {
				continue
			}
			cn := tools.CanonicalName(tool)
			if cn == "" {
				t.Errorf("provider %q produced a tool with an empty canonical name", pname)
				continue
			}
			produced++
			universe[cn] = struct{}{}
		}
		if produced == 0 {
			t.Errorf("toolprov provider %q Discover returned zero tools: the gate silently lost coverage", pname)
		}
	}
	return universe
}

// rp2ResolveAction mirrors Executor.checkPermission +
// PermissionChecker.CheckPermissionForAgent's lookup order: a
// ToolActionMap row, else the tool name itself becomes the action; the
// action resolves when BuiltinRules has a row for it, or when a fail-closed
// prefix classifier claims it (cua-driver.* / browser_* observation vs
// injection, acp_agent exact name). Anything else is the runtime's
// "Unknown action" denial.
func rp2ResolveAction(toolName string) (action string, resolvable bool) {
	action, ok := agent.ToolActionMap[toolName]
	if !ok {
		action = toolName
	}
	if _, known := security.BuiltinRules[action]; known {
		return action, true
	}
	if _, ok := security.ComputerUseRule(action); ok {
		return action, true
	}
	if _, ok := security.BrowserToolRule(action); ok {
		return action, true
	}
	if _, ok := security.ACPAgentRule(action, nil); ok {
		return action, true
	}
	return action, false
}

// rp2ResolvedAction returns the action a name resolves to (for failure
// messages only).
func rp2ResolvedAction(toolName string) string {
	action, _ := rp2ResolveAction(toolName)
	return action
}

// rp2InertAllowlist — registered/produced tool names that do NOT resolve
// to a permission rule on today's tree: the implemented + registered +
// inert bug class, each CONFIRMED by running this gate on 2026-11-14.
// These are FINDINGS, not fixes: adding the missing ToolActionMap row
// and/or BuiltinRules row is separate wiring work, tracked per entry.
// This list must SHRINK as findings close; adding a name without a
// finding id is a review reject, and a name that stops being inert must
// be deleted here (the staleness assertion below enforces it).
var rp2InertAllowlist = map[string]string{
	// No ToolActionMap row AND no identically-named rule: the name-fallback
	// action is unknown to BuiltinRules -> every call denied "Unknown
	// action". Fix precedent: json_extract / transcript_fetch (bughunt H7):
	// row in internal/agent/executor.go + rule in pkg/security/permissions.go
	// where needed, then delete the entry here.
	"ast_edit":                   "finding RP-01: registered behind code_intel enabled gate; no ToolActionMap row, no ast_edit rule -> inert",
	"resolve_ast_edit":           "finding RP-02: resolve-side AST edit (codetools AST lane); same missing row/rule as RP-01 -> inert",
	"cron_create":                "finding RP-03: scheduler provider Discover() contributes cron_create but executor.go has no row -> inert (schedule_* siblings landed; pause/resume/run_now are RP-04)",
	"schedule_pause":             "finding RP-04: scheduler family rows landed for create/list/get/delete only; pause/resume/run_now unmapped -> inert",
	"schedule_resume":            "finding RP-04: same scheduler-family gap as cron_create/schedule_pause",
	"schedule_run_now":           "finding RP-04: same scheduler-family gap as cron_create/schedule_pause",
	"debug":                      "finding RP-05: DAP debug tool registered unconditionally; no row -> identity action \"debug\" unknown -> inert",
	"git_commit":                 "finding RP-06: registered in registerBuiltinTools; no row/rule -> inert",
	"git_overview":               "finding RP-06: git_overview, same gap as git_commit",
	"hermes":                     "finding RP-07: hermes gateway tool arrives via the hermes provider; no row/rule -> inert",
	"memory_fact_search":         "finding RP-08: harness-eval leaf-12 fact-retrieval tool registered with the memory manager; no row/rule -> inert",
	"memory_get_version":         "finding RP-09: memory versioning family registered with the memory manager; no row/rule -> inert",
	"memory_get_version_history": "finding RP-09: memory versioning family, same gap as memory_get_version",
	"task_summarize":             "finding RP-10: transcript summarize tool registered with the summarizer lane; no row/rule -> inert",
	"tool_search":                "finding RP-11: deferred-schema discovery tool registered next to tool_view; no row/rule -> inert",
}

// rp2GatedRegistrationPins — ToolActionMap keys this hermetic boot does NOT
// produce because their registration site sits behind a default-off daemon
// gate this test deliberately leaves off (a dispatcher/orchestrator/
// session-store/multi-agent/employee/transport leg, an [mcp] server config,
// a code_intel.lsp server map, or the intake/plan-mode lanes). The row IS
// live in production when its gate opens and resolves to a rule, so this is
// NOT an inert-tool finding — only the reverse-direction gate of coverage
// here. A pinned key whose tool no gate can EVER register must be removed
// from this list (then the reverse assertion fails, which is the point).
var rp2GatedRegistrationPins = map[string]string{
	"ask":                    "gated: built-in ask tool needs the msgBus pending-question store wiring at the agent-loop site",
	"intake_save":            "gated: intake tool registers only with [intake].enabled (default false)",
	"send_agent_message":     "gated: inter-agent messaging wires after EmployeeMessageStore exists (multi-user/employee boot leg)",
	"inbox":                  "gated: same employee-message-store leg as send_agent_message",
	"request_handoff":        "gated: wires where the dispatcher + agent registry + message bus init (multi-agent boot leg)",
	"request_review":         "gated: same platform-tools leg as request_handoff",
	"delegate_task":          "gated: registerPlatformTools leg runs after Dispatcher init (the legacy direct-Register site predates the delegate provider)",
	"team_assign":            "gated: registerTeamTools runs only with the strategic/tactical orchestrator (multi-agent orchestrator leg)",
	"team_message":           "gated: same team leg as team_assign",
	"team_result":            "gated: same team leg as team_assign",
	"team_status":            "gated: same team leg as team_assign",
	"team_preset_create":     "gated: same team leg as team_assign",
	"platform_status":        "gated: registerPlatformTools runs after Dispatcher init (multi-agent boot leg)",
	"platform_tools":         "gated: same platform leg as platform_status",
	"platform_agents":        "gated: same platform leg as platform_status",
	"platform_team_create":   "gated: same platform leg as platform_status",
	"project_info":           "gated: platform-tools leg with the project manager wired",
	"template_clear":         "gated: registerTemplateTools only when the template registry is built (orchestrator leg)",
	"template_invoke":        "gated: same template leg as template_clear",
	"workspace_yield":        "gated: registerCollaborationTools only with the collaboration engine (multi-agent leg)",
	"mcp_servers":            "gated: registers only when the MCP manager is live ([mcp] block, default off)",
	"memory_delete":          "gated: memory-delete tool leg beyond the manager-init gate this boot arms",
	"entity_create":          "gated: knowledge-graph tools need a live memoryMgr.Graph() (memory-v2 leg)",
	"entity_link":            "gated: same graph leg as entity_create",
	"entity_query":           "gated: same graph leg as entity_create",
	"graph_stats":            "gated: same graph leg as entity_create",
	"compute_pagerank":       "gated: same graph leg as entity_create",
	"detect_communities":     "gated: same graph leg as entity_create",
	"community_siblings":     "gated: same graph leg as entity_create",
	"lsp_goto_definition":    "gated: code-intelligence LSP family registers only with configured LSP servers (code_intel.lsp.servers)",
	"lsp_find_references":    "gated: same LSP leg as lsp_goto_definition",
	"lsp_hover":              "gated: same LSP leg as lsp_goto_definition",
	"lsp_workspace_symbols":  "gated: same LSP leg as lsp_goto_definition",
	"lsp_diagnostics":        "gated: same LSP leg as lsp_goto_definition",
	"lsp_navigate":           "gated: same LSP leg as lsp_goto_definition",
}

// rp2AdminRuleAllowlist — BuiltinRules keys no registrable tool resolves
// to. They are reachable through non-model paths (an admin/manual security
// engine flow; see internal/agent/AGENTS.md and pkg/security seed-rule
// vocabulary), not part of the tool-registration bug class. Pinned here so
// a renamed/dead rule still trips the assertion; an entry whose rule became
// reachable by a registrable tool must be deleted (staleness assertion).
var rp2AdminRuleAllowlist = map[string]string{
	"install_package": "admin/manual security-engine path (no builtin model tool maps to it today; the installer flow reaches it directly)",
	"system_modify":   "admin/manual security-engine path (same posture as install_package)",
}

func TestRegisteredToolsPermissionParity(t *testing.T) {
	universe := rp2BootParityUniverse(t)
	names := make([]string, 0, len(universe))
	for n := range universe {
		names = append(names, n)
	}
	sort.Strings(names)

	// Floor: the gate must have actually run over a real production set. If
	// NewComponents stopped registering builtin tools, an empty/short set
	// would pass every assertion vacuously. (Measured universe on the
	// current tree: 96 names; floor guards a registration collapse.)
	const rp2Floor = 80
	if len(names) < rp2Floor {
		t.Fatalf("parity universe collected %d tool names, want >= %d: the registration walk lost coverage", len(names), rp2Floor)
	}

	// (1) FORWARD — registered => resolvable. A registered tool whose
	// resolved action has no rule is denied "Unknown action" on every model
	// call: implemented, registered, granted, INERT (the H7/C32/C33 class).
	var inert []string
	for _, name := range names {
		if _, ok := rp2ResolveAction(name); ok {
			continue
		}
		if _, exempt := rp2InertAllowlist[name]; exempt {
			continue
		}
		inert = append(inert, name+" (resolved action: "+rp2ResolvedAction(name)+")")
	}
	sort.Strings(inert)
	if len(inert) > 0 {
		t.Errorf("registered tool(s) resolve to NO permission rule - every model call fails \"Unknown action\" (deny-by-default) despite registration + grant: registered-but-inert (bughunt H7/C32/C33 class): %v\nFix: add the ToolActionMap row in internal/agent/executor.go and/or the action rule in pkg/security/permissions.go, then DELETE the name from rp2InertAllowlist in this file.", inert)
	}

	// The inert allowlist must shrink, never rot silently: a listed name
	// that stopped being inert (the fix landed) or stopped being registrable
	// must be deleted here.
	var healedInert []string
	for name := range rp2InertAllowlist {
		if _, registered := universe[name]; !registered {
			healedInert = append(healedInert, name+" (no longer registered: allowlist entry must be deleted)")
			continue
		}
		if _, ok := rp2ResolveAction(name); ok {
			healedInert = append(healedInert, name+" (now resolves: allowlist entry must be deleted)")
		}
	}
	sort.Strings(healedInert)
	if len(healedInert) > 0 {
		t.Errorf("rp2InertAllowlist is stale - the allowlist only shrinks: %v", healedInert)
	}

	// (2) REVERSE — no dead ToolActionMap rows. Every map key is either
	// produced by this boot or pinned as reachable behind a documented
	// default-off gate. A key in neither set is a row for a tool no gate
	// can ever register (a renamed/removed tool left behind).
	var dead []string
	for key := range agent.ToolActionMap {
		if _, live := universe[key]; live {
			continue
		}
		if _, pinned := rp2GatedRegistrationPins[key]; pinned {
			continue
		}
		dead = append(dead, key+"->"+agent.ToolActionMap[key])
	}
	sort.Strings(dead)
	if len(dead) > 0 {
		t.Errorf("ToolActionMap row(s) for a tool NO registration path can produce (dead mapping; a renamed/removed tool left behind): %v\nFix: delete the row in internal/agent/executor.go, or add the tool to rp2GatedRegistrationPins ONLY if a real default-off gate registers it.", dead)
	}
	var healedPins []string
	for key := range rp2GatedRegistrationPins {
		if _, live := universe[key]; live {
			healedPins = append(healedPins, key+" (now registrable in the default boot: pin must be deleted or the gate documented)")
		}
	}
	sort.Strings(healedPins)
	if len(healedPins) > 0 {
		t.Errorf("rp2GatedRegistrationPins is stale - pins only grow behind real gates: %v", healedPins)
	}

	// (3) RULES — every BuiltinRules key is reached by the resolved action
	// of a registrable tool (live or gate-pinned: the row binds when the
	// gate opens), or is allowlisted as an admin/manual-path action.
	reached := map[string]bool{}
	for name := range universe {
		if action, ok := rp2ResolveAction(name); ok {
			reached[action] = true
		}
	}
	for name := range rp2GatedRegistrationPins {
		if action, ok := rp2ResolveAction(name); ok {
			reached[action] = true
		}
	}
	var orphanRules []string
	for rule := range security.BuiltinRules {
		if reached[rule] {
			continue
		}
		if _, allowed := rp2AdminRuleAllowlist[rule]; allowed {
			continue
		}
		orphanRules = append(orphanRules, rule)
	}
	sort.Strings(orphanRules)
	if len(orphanRules) > 0 {
		t.Errorf("BuiltinRules action(s) that no registrable tool resolves to and no allowlist names (dead rule or renamed tool): %v\nFix: drop the rule, point a ToolActionMap row at it, or allowlist it with a reason.", orphanRules)
	}
	var healedAdminRules []string
	for rule := range rp2AdminRuleAllowlist {
		if reached[rule] {
			healedAdminRules = append(healedAdminRules, rule+" (now reached: allowlist entry must be deleted)")
		}
	}
	sort.Strings(healedAdminRules)
	if len(healedAdminRules) > 0 {
		t.Errorf("rp2AdminRuleAllowlist stale: %v", healedAdminRules)
	}

	t.Logf("registry parity universe: %d registrable tool names; %d allowlisted inert (findings open); %d gate-pinned map rows; %d allowlisted admin rules",
		len(names), len(rp2InertAllowlist), len(rp2GatedRegistrationPins), len(rp2AdminRuleAllowlist))
}
