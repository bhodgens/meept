package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agents"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// silentLogger returns a logger that discards output, for noise-free tests.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// writeAgentMD writes an AGENT.md file under a temp agents root.
func writeAgentMD(t *testing.T, root, id, frontmatter, body string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	content := "---\n" + frontmatter + "\n---\n\n" + body
	full := filepath.Join(dir, "AGENT.md")
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// newRegistryFromTempBundled creates an AgentRegistry that loads AGENT.md
// files from a temp "bundled" directory. Project/user/system tiers are
// cleared via WithTiers([]) so only the bundled path is scanned.
func newRegistryFromTempBundled(t *testing.T, bundledPath string) *AgentRegistry {
	t.Helper()
	// We can't pass WithTiers through RegistryConfig directly; instead we
	// construct the registry and call loadAgentDefinitions ourselves with
	// a discovery that has no user/system tiers. Use NewAgentRegistry with
	// only BundledAgentsPath set; the default tiers may add the user's
	// ~/.meept/agents dir but for tests on CI/dev machines without that
	// dir, only bundled files load. To make the test fully deterministic,
	// we manually run loadAgentDefinitions with a custom Discovery.
	r := &AgentRegistry{
		specs:           make(map[string]*AgentSpec),
		loops:           make(map[string]*AgentLoop),
		activeQueues:    make(map[string]*QueueEntry),
		logger:          silentLogger(),
		sharedConvStore: NewConversationStore(100),
	}
	discovery := agents.NewDiscovery(
		agents.WithDiscoveryLogger(r.logger),
		agents.WithTiers(nil), //nolint:staticcheck // intentional: skip default user/system tiers
		agents.WithBundledPath(bundledPath),
	)
	defs, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	for _, def := range defs {
		if !def.IsEnabled() {
			continue
		}
		r.mergeAgentDefinition(def)
	}
	return r
}

// --- assemblePurpose tests (Testing Strategy item 2) ---

func TestAssemblePurpose_BodyOnlyWhenNoComponents(t *testing.T) {
	r := &AgentRegistry{logger: silentLogger()}
	got := r.assemblePurpose(nil, "do the thing")
	if got != "do the thing" {
		t.Errorf("assemblePurpose(nil, body) = %q, want body verbatim", got)
	}
}

func TestAssemblePurpose_BodyOnlyWhenRegistryHasNoComponents(t *testing.T) {
	// Registry with nil ComponentRegistry → body alone, backward compatible.
	r := &AgentRegistry{logger: silentLogger(), components: nil}
	got := r.assemblePurpose([]string{"base.constitution"}, "body text")
	if got != "body text" {
		t.Errorf("nil ComponentRegistry should fall back to body; got %q", got)
	}
}

func TestAssemblePurpose_ComponentsWrapBody(t *testing.T) {
	// Build a ComponentRegistry from a temp prompts root with two components.
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "base", "constitution.md"), "# Constitution\n\nBe excellent.")
	mustWriteFile(t, filepath.Join(root, "capabilities", "memory.md"), "# Memory\n\nUse memory tools.")

	reg := agents.NewDefaultComponentRegistry(root, silentLogger())
	r := &AgentRegistry{logger: silentLogger(), components: reg}

	got := r.assemblePurpose(
		[]string{"base.constitution", "capabilities.memory"},
		"Do the work.",
	)

	// Components come first, each as a titled section; body is appended as
	// the "Purpose & Task Principles" section.
	if !strings.Contains(got, "# Constitution") {
		t.Errorf("missing Constitution section; got:\n%s", got)
	}
	if !strings.Contains(got, "# Memory") {
		t.Errorf("missing Memory section; got:\n%s", got)
	}
	if !strings.Contains(got, "# Purpose & Task Principles") {
		t.Errorf("missing body section header; got:\n%s", got)
	}
	if !strings.Contains(got, "Do the work.") {
		t.Errorf("missing body content; got:\n%s", got)
	}
	// Constitution must come before Memory, Memory before body.
	cIdx := strings.Index(got, "# Constitution")
	mIdx := strings.Index(got, "# Memory")
	bIdx := strings.Index(got, "# Purpose & Task Principles")
	if !(cIdx < mIdx && mIdx < bIdx) {
		t.Errorf("ordering wrong: c=%d m=%d b=%d", cIdx, mIdx, bIdx)
	}
}

func TestAssemblePurpose_MissingComponentSkipped(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "base", "constitution.md"), "# Constitution")
	reg := agents.NewDefaultComponentRegistry(root, silentLogger())
	r := &AgentRegistry{logger: silentLogger(), components: reg}

	// Missing component should be logged and skipped; the rest still assembles.
	got := r.assemblePurpose(
		[]string{"base.constitution", "does.not.exist", "body later"},
		"agent body",
	)
	if !strings.Contains(got, "# Constitution") {
		t.Errorf("constitution missing; got:\n%s", got)
	}
	if strings.Contains(got, "does.not.exist") {
		t.Errorf("missing component ID leaked into output; got:\n%s", got)
	}
	if !strings.Contains(got, "agent body") {
		t.Errorf("body missing; got:\n%s", got)
	}
}

// --- Disabled-agent filtering (Testing Strategy item 4) ---

func TestLoadAgentDefinitions_DisabledFiltered(t *testing.T) {
	root := t.TempDir()
	writeAgentMD(t, root, "enabled-one",
		"id: enabled-one\nname: Enabled\nrole: executor\nenabled: true",
		"body")
	writeAgentMD(t, root, "disabled-one",
		"id: disabled-one\nname: Disabled\nrole: executor\nenabled: false",
		"body")

	r := newRegistryFromTempBundled(t, root)
	if _, ok := r.GetSpec("enabled-one"); !ok {
		t.Error("expected enabled-one to be loaded")
	}
	if _, ok := r.GetSpec("disabled-one"); ok {
		t.Error("expected disabled-one to be filtered out")
	}
}

// --- Minimal AGENT.md (Testing Strategy item 5) ---

func TestLoadAgentDefinitions_MinimalAgentMD(t *testing.T) {
	root := t.TempDir()
	// Just id + body: should load with sensible defaults.
	writeAgentMD(t, root, "minimal",
		"id: minimal",
		"just a body")

	r := newRegistryFromTempBundled(t, root)
	spec, ok := r.GetSpec("minimal")
	if !ok {
		t.Fatal("expected minimal agent to load")
	}
	if spec.Role != RoleExecutor {
		t.Errorf("default Role = %q, want %q", spec.Role, RoleExecutor)
	}
	if spec.Name != "minimal" {
		t.Errorf("default Name = %q, want %q", spec.Name, "minimal")
	}
	if !spec.Enabled {
		t.Error("default Enabled should be true")
	}
	// Default constraints should be populated.
	if spec.Constraints.MaxIterations != DefaultConstraints().MaxIterations {
		t.Errorf("MaxIterations = %d, want default %d",
			spec.Constraints.MaxIterations, DefaultConstraints().MaxIterations)
	}
	if spec.Purpose != "just a body" {
		t.Errorf("Purpose = %q, want body verbatim", spec.Purpose)
	}
}

// --- All 14 bundled agents load (Testing Strategy item 3) ---

func TestLoadAgentDefinitions_AllBundled(t *testing.T) {
	// Points at the repo's bundled config/agents dir. Skips if not present.
	bundled := "../../config/agents"
	if _, err := os.Stat(bundled); err != nil {
		t.Skipf("bundled agents dir not available: %s", bundled)
	}

	r := &AgentRegistry{
		specs:           make(map[string]*AgentSpec),
		loops:           make(map[string]*AgentLoop),
		activeQueues:    make(map[string]*QueueEntry),
		logger:          silentLogger(),
		sharedConvStore: NewConversationStore(100),
	}
	discovery := agents.NewDiscovery(
		agents.WithDiscoveryLogger(r.logger),
		agents.WithTiers(nil), //nolint:staticcheck // skip user/system dirs for determinism
		agents.WithBundledPath(bundled),
	)
	defs, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	for _, def := range defs {
		if !def.IsEnabled() {
			continue
		}
		r.mergeAgentDefinition(def)
	}

	expected := []string{
		config.AgentIDDispatcher,
		config.AgentIDChat,
		config.AgentIDCoder,
		config.AgentIDDebugger,
		config.AgentIDPlanner,
		config.AgentIDAnalyst,
		config.AgentIDResearcher,
		config.AgentIDCommitter,
		config.AgentIDScheduler,
		"code-reviewer", "test-reviewer", "debug-reviewer",
		"analyst-reviewer", "planner-reviewer",
		// Plan 2: Agent Roster Extension knowledge-work specialists.
		config.AgentIDWriter,
		config.AgentIDArchitect,
		config.AgentIDSkeptic,
		config.AgentIDLibrarian,
	}
	for _, id := range expected {
		spec, ok := r.GetSpec(id)
		if !ok {
			t.Errorf("expected bundled agent %q to load", id)
			continue
		}
		if spec.Name == "" {
			t.Errorf("agent %q has empty Name", id)
		}
		if spec.Purpose == "" {
			t.Errorf("agent %q has empty Purpose", id)
		}
	}

	// Reviewer agents must have role=reviewer + reviews_domain set.
	reviewers := map[string]string{
		"code-reviewer":    "code",
		"test-reviewer":    "test",
		"debug-reviewer":   "debug",
		"analyst-reviewer": "analysis",
		"planner-reviewer": "plan",
	}
	for id, domain := range reviewers {
		spec, ok := r.GetSpec(id)
		if !ok {
			continue
		}
		if spec.Role != RoleReviewer {
			t.Errorf("%s.Role = %q, want %q", id, spec.Role, RoleReviewer)
		}
		if spec.ReviewsDomain != domain {
			t.Errorf("%s.ReviewsDomain = %q, want %q", id, spec.ReviewsDomain, domain)
		}
	}

	// Researcher should have web_fetch / web_search tools.
	if spec, ok := r.GetSpec(config.AgentIDResearcher); ok {
		for _, want := range []string{"web_fetch", "web_search"} {
			if !spec.HasTool(want) {
				t.Errorf("researcher missing additional tool %q (has %v)", want, spec.AdditionalTools)
			}
		}
	}

	// Plan 2: verify researcher has litreview/dossier/code-tour skills,
	// analyst has competitive-teardown, librarian has its three skills,
	// and skeptic has grill-me.
	skillChecks := map[string][]string{
		config.AgentIDResearcher: {"litreview", "dossier", "code-tour"},
		config.AgentIDAnalyst:    {"competitive-teardown"},
		config.AgentIDSkeptic:    {"grill-me"},
		config.AgentIDLibrarian:  {"librarian-backlog-mining", "librarian-reflection-surfacing", "librarian-tag-hygiene"},
	}
	for agentID, skills := range skillChecks {
		spec, ok := r.GetSpec(agentID)
		if !ok {
			t.Errorf("expected bundled agent %q to load (skill check)", agentID)
			continue
		}
		for _, want := range skills {
			if !spec.HasSkill(want) {
				t.Errorf("agent %q missing available_skill %q (has %v)", agentID, want, spec.AvailableSkills)
			}
		}
	}
}

// --- Reviewer routing (Testing Strategy item 6) ---

func TestSelectReviewer_DynamicByDomain(t *testing.T) {
	// Build a registry with reviewer specs covering each domain.
	r := &AgentRegistry{
		specs:           make(map[string]*AgentSpec),
		loops:           make(map[string]*AgentLoop),
		activeQueues:    make(map[string]*QueueEntry),
		logger:          silentLogger(),
		sharedConvStore: NewConversationStore(100),
	}
	reviewers := map[string]string{
		"code-reviewer":    "code",
		"test-reviewer":    "test",
		"debug-reviewer":   "debug",
		"analyst-reviewer": "analysis",
		"planner-reviewer": "plan",
	}
	for id, domain := range reviewers {
		spec := &AgentSpec{
			ID:            id,
			Name:          id,
			Role:          RoleReviewer,
			ReviewsDomain: domain,
			Enabled:       true,
		}
		r.specs[id] = spec
	}

	policy := &ReviewPolicy{
		Registry: r,
	}

	cases := []struct {
		name    string
		agentID string
		want    string
	}{
		{"coder steps → code-reviewer", config.AgentIDCoder, "code-reviewer"},
		{"debugger steps → debug-reviewer", config.AgentIDDebugger, "debug-reviewer"},
		{"planner steps → planner-reviewer", config.AgentIDPlanner, "planner-reviewer"},
		{"analyst steps → analyst-reviewer", config.AgentIDAnalyst, "analyst-reviewer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			step := &task.TaskStep{AgentID: tc.agentID}
			got := policy.SelectReviewer(step)
			if got != tc.want {
				t.Errorf("SelectReviewer(agent=%q) = %q, want %q",
					tc.agentID, got, tc.want)
			}
		})
	}
}

func TestSelectReviewer_FallsBackToTestReviewer(t *testing.T) {
	// Empty registry → no domain match → falls back to "test-reviewer".
	r := &AgentRegistry{
		specs:           make(map[string]*AgentSpec),
		loops:           make(map[string]*AgentLoop),
		activeQueues:    make(map[string]*QueueEntry),
		logger:          silentLogger(),
		sharedConvStore: NewConversationStore(100),
	}
	policy := &ReviewPolicy{
		Registry: r,
	}
	step := &task.TaskStep{AgentID: config.AgentIDCoder}
	got := policy.SelectReviewer(step)
	if got != "test-reviewer" {
		t.Errorf("expected fallback to test-reviewer, got %q", got)
	}
}

// --- findReviewerByDomain direct test ---

func TestFindReviewerByDomain(t *testing.T) {
	r := &AgentRegistry{
		specs:           make(map[string]*AgentSpec),
		logger:          silentLogger(),
		sharedConvStore: NewConversationStore(100),
	}
	// Seed two reviewers + a disabled one + a non-reviewer.
	r.specs["code-reviewer"] = &AgentSpec{ID: "code-reviewer", Role: RoleReviewer, ReviewsDomain: "code", Enabled: true}
	r.specs["plan-reviewer"] = &AgentSpec{ID: "plan-reviewer", Role: RoleReviewer, ReviewsDomain: "plan", Enabled: true}
	r.specs["disabled-rev"] = &AgentSpec{ID: "disabled-rev", Role: RoleReviewer, ReviewsDomain: "code", Enabled: false}
	r.specs["coder"] = &AgentSpec{ID: "coder", Role: RoleExecutor, Enabled: true} // not a reviewer

	if got := r.findReviewerByDomain("code"); got != "code-reviewer" {
		t.Errorf("findReviewerByDomain(code) = %q, want code-reviewer", got)
	}
	if got := r.findReviewerByDomain("plan"); got != "plan-reviewer" {
		t.Errorf("findReviewerByDomain(plan) = %q, want plan-reviewer", got)
	}
	if got := r.findReviewerByDomain("nonexistent"); got != "" {
		t.Errorf("findReviewerByDomain(nonexistent) = %q, want empty", got)
	}
	if got := r.findReviewerByDomain(""); got != "" {
		t.Errorf("findReviewerByDomain(empty) = %q, want empty", got)
	}
}

// --- helper ---

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// --- Dispatcher routing table vs roster consistency (Testing Strategy item 8) ---
//
// The dispatcher AGENT.md contains a baseline routing table. Every agent ID
// mentioned there must exist in the loaded roster (no phantoms). This test
// parses the dispatcher body for backticked agent IDs and checks each one
// resolves against the bundled roster.

func TestDispatcherRoutingTableMatchesRoster(t *testing.T) {
	bundledAgents := "../../config/agents"
	if _, err := os.Stat(bundledAgents); err != nil {
		t.Skipf("bundled agents dir not available: %s", bundledAgents)
	}

	// Load the roster.
	r := &AgentRegistry{
		specs:           make(map[string]*AgentSpec),
		logger:          silentLogger(),
		sharedConvStore: NewConversationStore(100),
	}
	discovery := agents.NewDiscovery(
		agents.WithDiscoveryLogger(r.logger),
		agents.WithTiers(nil), //nolint:staticcheck // skip user/system dirs for determinism
		agents.WithBundledPath(bundledAgents),
	)
	defs, err := discovery.Discover()
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	for _, def := range defs {
		if !def.IsEnabled() {
			continue
		}
		r.mergeAgentDefinition(def)
	}

	// Find the dispatcher body.
	dispatcherPath := filepath.Join(bundledAgents, "dispatcher", "AGENT.md")
	body, err := os.ReadFile(dispatcherPath)
	if err != nil {
		t.Fatalf("read dispatcher AGENT.md: %v", err)
	}

	// Pull every backticked token from the body. The routing table lists
	// route targets as `agent-id`. Also collect a known-good allowlist of
	// non-agent backtick tokens that appear in the dispatcher body so we
	// don't false-flag tool or field names.
	allowlist := map[string]bool{
		"platform_agents":        true,
		"platform_tools":         true,
		"platform_status":        true,
		"delegate_task":          true,
		"memory_search":          true,
		"memory_refs":            true,
		"context_query":          true,
		"inherited_from":         true,
		"agent_id":               true,
		"message":                true,
	}

	// Extract `token` occurrences.
	var phantom []string
	for i := 0; i < len(body); i++ {
		if body[i] != '`' {
			continue
		}
		end := -1
		for j := i + 1; j < len(body); j++ {
			if body[j] == '`' {
				end = j
				break
			}
		}
		if end < 0 {
			break
		}
		token := string(body[i+1 : end])
		i = end
		if token == "" || allowlist[token] {
			continue
		}
		// Heuristic: agent IDs contain only lowercase letters, digits, and hyphens.
		if !isAgentIDLike(token) {
			continue
		}
		if _, ok := r.GetSpec(token); !ok {
			phantom = append(phantom, token)
		}
	}
	if len(phantom) > 0 {
		t.Errorf("dispatcher AGENT.md references agent IDs not in roster: %v", phantom)
	}

	// Sanity: the routing table MUST mention at least these canonical agents.
	bodyStr := string(body)
	for _, want := range []string{
		config.AgentIDCoder,
		config.AgentIDDebugger,
		config.AgentIDResearcher,
		config.AgentIDAnalyst,
		config.AgentIDPlanner,
		config.AgentIDCommitter,
		config.AgentIDScheduler,
		config.AgentIDChat,
		"code-reviewer",
		// Plan 2: new knowledge-work agents appear in the routing table.
		config.AgentIDWriter,
		config.AgentIDArchitect,
		config.AgentIDSkeptic,
		config.AgentIDLibrarian,
	} {
		// Search for the agent ID wrapped in backticks (the routing table format).
		needle := "`" + want + "`"
		if !strings.Contains(bodyStr, needle) {
			t.Errorf("dispatcher routing table missing agent %q (looked for %q)", want, needle)
		}
	}
}

// isAgentIDLike returns true if the token matches the agent ID shape:
// lowercase letters, digits, hyphens; at least one letter; not a known keyword.
func isAgentIDLike(s string) bool {
	if s == "" {
		return false
	}
	hasLetter := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			hasLetter = true
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return hasLetter
}

// --- GetModelConfig tests ---

// newTestRegistryWithResolver builds a minimal AgentRegistry wired to a
// Resolver whose default model carries a non-zero ContextLimit. It mirrors
// the direct-struct-literal pattern used by other tests in this file.
func newTestRegistryWithResolver(t *testing.T) *AgentRegistry {
	t.Helper()
	cfg := &llm.ProvidersConfig{
		Model: "testprov/default-m",
		Providers: map[string]llm.ProviderConfig{
			"testprov": {
				API: "openai",
				Models: map[string]llm.ModelDef{
					"default-m": {Name: "default-m", ContextLimit: 8192},
					"coder-m":   {Name: "coder-m", ContextLimit: 16384},
				},
			},
		},
	}
	resolver := llm.NewResolver(cfg, silentLogger())
	r := &AgentRegistry{
		specs:           make(map[string]*AgentSpec),
		loops:           make(map[string]*AgentLoop),
		activeQueues:    make(map[string]*QueueEntry),
		resolver:        resolver,
		logger:          silentLogger(),
		sharedConvStore: NewConversationStore(100),
	}
	// "coder" has an explicit model ref; "chat" falls back to default.
	r.specs["coder"] = &AgentSpec{ID: "coder", Model: "testprov/coder-m", Enabled: true}
	r.specs["chat"] = &AgentSpec{ID: "chat", Model: "", Enabled: true}
	return r
}

func TestAgentRegistry_GetModelConfig(t *testing.T) {
	r := newTestRegistryWithResolver(t)

	// Agent with explicit model ref.
	cfg, err := r.GetModelConfig("coder")
	if err != nil {
		t.Fatalf("GetModelConfig(coder): %v", err)
	}
	if cfg == nil {
		t.Fatal("GetModelConfig(coder) returned nil cfg")
	}
	if cfg.ContextLimit != 16384 {
		t.Errorf("coder ContextLimit = %d, want 16384", cfg.ContextLimit)
	}

	// Agent with empty Model → resolver default.
	cfgDefault, err := r.GetModelConfig("chat")
	if err != nil {
		t.Fatalf("GetModelConfig(chat): %v", err)
	}
	if cfgDefault == nil {
		t.Fatal("GetModelConfig(chat) returned nil cfg")
	}
	if cfgDefault.ContextLimit != 8192 {
		t.Errorf("chat (default) ContextLimit = %d, want 8192", cfgDefault.ContextLimit)
	}
}

func TestAgentRegistry_GetModelConfig_UnknownAgent(t *testing.T) {
	r := newTestRegistryWithResolver(t)
	_, err := r.GetModelConfig("nonexistent")
	if err == nil {
		t.Fatal("want error for unknown agent, got nil")
	}
}

// TestAgentRegistry_GetModelConfig_UnresolvableModel verifies that an agent
// whose Model ref points to a provider/model that the resolver cannot resolve
// returns (nil, error) instead of (nil, nil). Without the nil guard in
// GetModelConfig, callers would deref the nil cfg and panic — the same
// nil/nil anti-pattern documented in mcp/client.go:227.
func TestAgentRegistry_GetModelConfig_UnresolvableModel(t *testing.T) {
	r := newTestRegistryWithResolver(t)
	// "ghostprov" does not exist in newTestRegistryWithResolver's ProvidersConfig,
	// so ResolveRef returns nil. The test registry's resolver is non-nil, which
	// isolates this test from the resolver-nil branch.
	r.specs["ghost"] = &AgentSpec{ID: "ghost", Model: "ghostprov/missing-m", Enabled: true}
	cfg, err := r.GetModelConfig("ghost")
	if err == nil {
		t.Fatal("want error for unresolvable model ref; got nil")
	}
	if cfg != nil {
		t.Errorf("want nil cfg; got %+v", cfg)
	}
}
