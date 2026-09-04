package agent

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agents"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/tools"
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
		loops:           make(map[string]map[string]*AgentLoop),
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
		loops:           make(map[string]map[string]*AgentLoop),
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
		config.AgentIDImageGen,
		config.AgentIDVideoGen,
		config.AgentIDImageID,
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
		config.AgentIDImageGen:   {"image-prompt-enhance"},
		config.AgentIDVideoGen:   {"video-prompt-enhance"},
		config.AgentIDImageID:    {"image-identify"},
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

	if spec, ok := r.GetSpec(config.AgentIDImageGen); ok {
		if spec.EnhancerModel != "small" {
			t.Errorf("image-gen EnhancerModel = %q, want small", spec.EnhancerModel)
		}
		if !spec.HasTool("generate_image") {
			t.Errorf("image-gen missing generate_image (has %v)", spec.AdditionalTools)
		}
	}
	if spec, ok := r.GetSpec(config.AgentIDVideoGen); ok {
		if spec.EnhancerModel != "small" {
			t.Errorf("video-gen EnhancerModel = %q, want small", spec.EnhancerModel)
		}
		if !spec.HasTool("generate_video") {
			t.Errorf("video-gen missing generate_video (has %v)", spec.AdditionalTools)
		}
	}
}

// --- Reviewer routing (Testing Strategy item 6) ---

func TestSelectReviewer_DynamicByDomain(t *testing.T) {
	// Build a registry with reviewer specs covering each domain.
	r := &AgentRegistry{
		specs:           make(map[string]*AgentSpec),
		loops:           make(map[string]map[string]*AgentLoop),
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
		loops:           make(map[string]map[string]*AgentLoop),
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
		"platform_agents": true,
		"platform_tools":  true,
		"platform_status": true,
		"delegate_task":   true,
		"memory_search":   true,
		"memory_refs":     true,
		"context_query":   true,
		"inherited_from":  true,
		"agent_id":        true,
		"message":         true,
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
		config.AgentIDImageGen,
		config.AgentIDVideoGen,
		config.AgentIDImageID,
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
		loops:           make(map[string]map[string]*AgentLoop),
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

// --- per-task-per-agent loop keying tests (Plan B Task 4) ---
//
// newTestRegistryForTask builds a minimal AgentRegistry with a few executor
// specs so GetForTask has something to lazy-create loops from. Loops created
// here have no LLM/bus/tools wired, but that's fine — we only test identity
// and bucketing, not loop execution.
//
// Named with the ForTask suffix to avoid colliding with the pre-existing
// no-arg newTestRegistry() in registry_queue_test.go.
func newTestRegistryForTask(t *testing.T) *AgentRegistry {
	t.Helper()
	r := &AgentRegistry{
		specs:           make(map[string]*AgentSpec),
		loops:           make(map[string]map[string]*AgentLoop),
		activeQueues:    make(map[string]*QueueEntry),
		logger:          silentLogger(),
		sharedConvStore: NewConversationStore(100),
	}
	r.specs["coder"] = &AgentSpec{ID: "coder", Name: "coder", Role: RoleExecutor, Enabled: true}
	r.specs["debugger"] = &AgentSpec{ID: "debugger", Name: "debugger", Role: RoleExecutor, Enabled: true}
	return r
}

func TestAgentRegistry_GetForTask_DistinctLoops(t *testing.T) {
	reg := newTestRegistryForTask(t)
	loop1, err := reg.GetForTask("coder", "task-1")
	if err != nil {
		t.Fatalf("GetForTask task-1: %v", err)
	}
	loop2, err := reg.GetForTask("coder", "task-2")
	if err != nil {
		t.Fatalf("GetForTask task-2: %v", err)
	}
	if loop1 == loop2 {
		t.Error("same agentID + different taskIDs returned same loop")
	}
}

func TestAgentRegistry_GetForTask_SameTaskReturnsSameLoop(t *testing.T) {
	reg := newTestRegistryForTask(t)
	loop1, _ := reg.GetForTask("coder", "task-1")
	loop2, _ := reg.GetForTask("coder", "task-1")
	if loop1 != loop2 {
		t.Error("same (agent, task) returned different loops")
	}
}

func TestAgentRegistry_GetForTask_EmptyTaskIDDefaults(t *testing.T) {
	reg := newTestRegistryForTask(t)
	loop1, _ := reg.GetForTask("coder", "")
	loop2, _ := reg.GetForTask("coder", "_default")
	if loop1 != loop2 {
		t.Error("empty taskID should default to _default")
	}
}

func TestAgentRegistry_ReleaseTaskLoops(t *testing.T) {
	reg := newTestRegistryForTask(t)
	reg.GetForTask("coder", "task-1")
	reg.GetForTask("debugger", "task-1")
	reg.ReleaseTaskLoops("task-1")
	// After release, new GetForTask should create a fresh loop.
	loop1, _ := reg.GetForTask("coder", "task-1")
	if loop1 == nil {
		t.Error("GetForTask returned nil after release")
	}
}

func TestAgentRegistry_Get_BackwardCompat(t *testing.T) {
	reg := newTestRegistryForTask(t)
	loop, err := reg.Get("coder")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if loop == nil {
		t.Error("Get returned nil")
	}
}

func TestAgentRegistry_ReleaseTaskLoops_EmptyTaskID_Noop(t *testing.T) {
	reg := newTestRegistryForTask(t)
	reg.GetForTask("coder", "task-1")
	// Empty taskID should be a no-op (doesn't delete anything).
	reg.ReleaseTaskLoops("")
	loop, _ := reg.GetForTask("coder", "task-1")
	if loop == nil {
		t.Error("empty taskID release should not delete existing loops")
	}
}

// --- escalation_model config surface (llm-resilience-forest tree 01 leaf 01-spec-config) ---

func TestEscalationModel_DefinitionToSpec(t *testing.T) {
	r := &AgentRegistry{logger: silentLogger()}
	def := &agents.AgentDefinition{
		AgentMetadata: agents.AgentMetadata{
			ID:              "esc-agent",
			Name:            "Escalation Agent",
			Role:            "executor",
			EscalationModel: "smarter-alias",
		},
	}

	spec := r.definitionToSpec(def)
	if spec.EscalationModel != "smarter-alias" {
		t.Errorf("definitionToSpec EscalationModel = %q, want %q", spec.EscalationModel, "smarter-alias")
	}

	// Empty definition field maps to empty spec field (disabled).
	def.EscalationModel = ""
	if got := r.definitionToSpec(def).EscalationModel; got != "" {
		t.Errorf("definitionToSpec EscalationModel = %q, want empty (disabled)", got)
	}
}

func TestEscalationModel_MergeSpec_Reload(t *testing.T) {
	// Re-load scenario: a spec already carries escalation_model from a prior
	// AGENT.md load; mergeSpec must preserve it.
	r := &AgentRegistry{logger: silentLogger()}
	base := &AgentSpec{
		ID:              "esc-agent",
		Name:            "Escalation Agent",
		Role:            RoleExecutor,
		Enabled:         true,
		EscalationModel: "smarter-alias",
	}
	def := &agents.AgentDefinition{
		AgentMetadata: agents.AgentMetadata{ID: "esc-agent", Name: "Escalation Agent", Role: "executor"},
	}

	merged := r.mergeSpec(base, def)
	if merged.EscalationModel != "smarter-alias" {
		t.Errorf("mergeSpec EscalationModel = %q, want %q (preserve base)", merged.EscalationModel, "smarter-alias")
	}

	// A re-load carrying its own value overrides the base (prefer AGENT.md).
	def.EscalationModel = "provider/other-model"
	merged = r.mergeSpec(base, def)
	if merged.EscalationModel != "provider/other-model" {
		t.Errorf("mergeSpec EscalationModel = %q, want %q (prefer AGENT.md)", merged.EscalationModel, "provider/other-model")
	}
}

func TestEscalationModel_FrontmatterToSpec(t *testing.T) {
	frontmatter := "---\nid: esc-agent\nname: Escalation Agent\nrole: executor\nescalation_model: my-alias\n---\n\n# Escalation Agent\n\nBody."
	def, err := agents.ParseAgentText(frontmatter)
	if err != nil {
		t.Fatalf("ParseAgentText failed: %v", err)
	}

	r := &AgentRegistry{logger: silentLogger()}
	spec := r.definitionToSpec(def)
	if spec.EscalationModel != "my-alias" {
		t.Errorf("spec.EscalationModel = %q, want %q (frontmatter → spec)", spec.EscalationModel, "my-alias")
	}
}

func TestVerification_MergeSpec_Reload(t *testing.T) {
	// Re-load scenario: a spec already carries a verification config from a
	// prior AGENT.md load; mergeSpec must preserve it when the re-loaded
	// frontmatter omits the verification block (D16 gap fix).
	r := &AgentRegistry{logger: silentLogger()}
	base := &AgentSpec{
		ID:      "ver-agent",
		Name:    "Verification Agent",
		Role:    RoleExecutor,
		Enabled: true,
		Verification: VerificationConfig{
			Enabled:     true,
			Model:       "verifier-model",
			AutoTrigger: true,
			MaxFixLoops: 5,
		},
	}
	def := &agents.AgentDefinition{
		AgentMetadata: agents.AgentMetadata{ID: "ver-agent", Name: "Verification Agent", Role: "executor"},
	}

	merged := r.mergeSpec(base, def)
	if merged.Verification.MaxFixLoops != 5 {
		t.Errorf("mergeSpec Verification.MaxFixLoops = %d, want 5 (preserve base on omission)",
			merged.Verification.MaxFixLoops)
	}
	if merged.Verification.Model != "verifier-model" {
		t.Errorf("mergeSpec Verification.Model = %q, want %q (preserve base on omission)",
			merged.Verification.Model, "verifier-model")
	}

	// A re-load carrying its own verification block overrides the base.
	enabled := true
	def.Verification = &agents.VerificationMetadata{
		Enabled:     &enabled,
		Model:       "new-verifier",
		MaxFixLoops: 7,
	}
	merged = r.mergeSpec(base, def)
	if merged.Verification.MaxFixLoops != 7 {
		t.Errorf("mergeSpec Verification.MaxFixLoops = %d, want 7 (prefer AGENT.md)",
			merged.Verification.MaxFixLoops)
	}
	if merged.Verification.Model != "new-verifier" {
		t.Errorf("mergeSpec Verification.Model = %q, want %q (prefer AGENT.md)",
			merged.Verification.Model, "new-verifier")
	}
	if !merged.Verification.Enabled {
		t.Errorf("mergeSpec Verification.Enabled = false, want true")
	}
}

func TestGate_MergeSpec_Reload(t *testing.T) {
	// Re-load scenario: a spec already carries a roster gate from a prior
	// AGENT.md load; mergeSpec must preserve it when the re-loaded
	// frontmatter omits the gate block (D16 gap fix).
	r := &AgentRegistry{logger: silentLogger()}
	base := &AgentSpec{
		ID:      "gate-agent",
		Name:    "Gate Agent",
		Role:    RoleExecutor,
		Enabled: true,
		Gate: &RosterGateConfig{
			Command:           "go test ./...",
			TimeoutSeconds:    120,
			SkipWhenUnchanged: true,
		},
	}
	def := &agents.AgentDefinition{
		AgentMetadata: agents.AgentMetadata{ID: "gate-agent", Name: "Gate Agent", Role: "executor"},
	}

	merged := r.mergeSpec(base, def)
	if merged.Gate == nil {
		t.Fatalf("mergeSpec Gate = nil, want preserved base gate (omission keeps current)")
	}
	if merged.Gate.Command != "go test ./..." {
		t.Errorf("mergeSpec Gate.Command = %q, want %q (preserve base on omission)",
			merged.Gate.Command, "go test ./...")
	}
	if merged.Gate.TimeoutSeconds != 120 {
		t.Errorf("mergeSpec Gate.TimeoutSeconds = %d, want 120 (preserve base on omission)",
			merged.Gate.TimeoutSeconds)
	}

	// A re-load carrying its own gate block overrides the base (prefer
	// AGENT.md). Parsed from real frontmatter so skip_when_unchanged: false
	// is explicitly-present (skipExplicit) and NormalizeGateDefaults
	// preserves it — programmatic GateMetadata construction normalizes the
	// plain bool back to true (documented caveat, agents.GateMetadata).
	gateFM := "---\nid: gate-agent\nname: Gate Agent\nrole: executor\ngate:\n  command: make lint\n  timeout_seconds: 60\n  skip_when_unchanged: false\n---\n\n# Gate Agent\n\nBody."
	parsed, err := agents.ParseAgentText(gateFM)
	if err != nil {
		t.Fatalf("ParseAgentText failed: %v", err)
	}
	merged = r.mergeSpec(base, parsed)
	if merged.Gate == nil {
		t.Fatalf("mergeSpec Gate = nil, want new gate (prefer AGENT.md)")
	}
	if merged.Gate.Command != "make lint" {
		t.Errorf("mergeSpec Gate.Command = %q, want %q (prefer AGENT.md)",
			merged.Gate.Command, "make lint")
	}
	if merged.Gate.TimeoutSeconds != 60 {
		t.Errorf("mergeSpec Gate.TimeoutSeconds = %d, want 60 (prefer AGENT.md)",
			merged.Gate.TimeoutSeconds)
	}
	if merged.Gate.SkipWhenUnchanged {
		t.Errorf("mergeSpec Gate.SkipWhenUnchanged = true, want false (prefer AGENT.md)")
	}
}

// --- Task-loop schema-mode wiring (chat-dispatch-ux leaf 04) ---

// taskSchemaTool is a fixture tool for registry schema-mode tests. It is NOT
// in DefaultAlwaysFullTools() and carries a non-empty parameter schema so
// stubbing is observable: under indexed mode its definition collapses to an
// empty object schema with a " use tool_view{name}." description suffix.
type taskSchemaTool struct {
	name string
}

func (t *taskSchemaTool) Name() string        { return t.name }
func (t *taskSchemaTool) Description() string { return "task schema fixture tool" }
func (t *taskSchemaTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"q": {Type: "string", Description: "the query"},
		},
		Required: []string{"q"},
	}
}
func (t *taskSchemaTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	return map[string]any{"ok": true}, nil
}
func (t *taskSchemaTool) IsReadOnly(map[string]any) bool        { return true }
func (t *taskSchemaTool) IsConcurrencySafe(map[string]any) bool { return true }

// newTestRegistryWithTaskTools builds an AgentRegistry wired to a real
// *tools.Registry carrying the always-full core tool "shell" plus a non-core
// fixture "task_schema_extra". The coder spec additionally allows the
// fixture, so the loop's FilteredToolRegistry exposes both through the
// shared parent.
func newTestRegistryWithTaskTools(t *testing.T) *AgentRegistry {
	t.Helper()
	toolReg := tools.NewRegistry(nil)
	toolReg.Register(&taskSchemaTool{name: "shell"})
	toolReg.Register(&taskSchemaTool{name: "task_schema_extra"})
	r := &AgentRegistry{
		specs:           make(map[string]*AgentSpec),
		loops:           make(map[string]map[string]*AgentLoop),
		activeQueues:    make(map[string]*QueueEntry),
		logger:          silentLogger(),
		sharedConvStore: NewConversationStore(100),
		tools:           toolReg,
	}
	r.specs["coder"] = &AgentSpec{
		ID:              "coder",
		Name:            "coder",
		Role:            RoleExecutor,
		Enabled:         true,
		AdditionalTools: []string{"shell", "task_schema_extra"},
	}
	return r
}

// findTaskSchemaDef returns the definition for name among defs, failing the
// test if absent. Mirrors findSchemaDef / findWiringDef in the sibling test
// files so assertion style stays consistent.
func findTaskSchemaDef(t *testing.T, defs []llm.ToolDefinition, name string) llm.ToolDefinition {
	t.Helper()
	for _, d := range defs {
		if d.Function.Name == name {
			return d
		}
	}
	t.Fatalf("tool %q not found among %d definitions", name, len(defs))
	return llm.ToolDefinition{}
}

// TestAgentRegistry_TaskScopedLoopGetsIndexedSchema verifies that a
// task-scoped loop built via GetForTask inherits the registry's schema-mode
// config: the non-core tool ships a stubbed empty schema with a tool_view
// expansion instruction, while the always-full core tool keeps its schema.
func TestAgentRegistry_TaskScopedLoopGetsIndexedSchema(t *testing.T) {
	r := newTestRegistryWithTaskTools(t)
	r.SetSchemaModeConfig(config.AgentToolsConfig{}) // zero value = indexed default

	loop, err := r.GetForTask("coder", "task-schema-1")
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}

	// The loop's registry is a FilteredToolRegistry wrapping the shared
	// parent; stubbing lives on the parent and is visible through the
	// filtered view's GetDefinitions.
	defs := loopRegistryDefinitions(t, loop)

	shell := findTaskSchemaDef(t, defs, "shell")
	if len(shell.Function.Parameters.Properties) == 0 {
		t.Error(`always-full core tool "shell" must keep its parameter properties`)
	}
	if len(shell.Function.Parameters.Required) == 0 {
		t.Error(`always-full core tool "shell" must keep its required list`)
	}

	extra := findTaskSchemaDef(t, defs, "task_schema_extra")
	if len(extra.Function.Parameters.Properties) != 0 {
		t.Errorf("non-core tool must be stubbed to an empty schema under indexed mode; got %d properties",
			len(extra.Function.Parameters.Properties))
	}
	if !strings.Contains(extra.Function.Description, " use tool_view{task_schema_extra}.") {
		t.Errorf("stubbed description %q must contain tool_view expansion instruction",
			extra.Function.Description)
	}
}

// TestAgentRegistry_SchemaModeConfigFullRestoresLegacy verifies that
// schema_mode "full" ships complete schemas for every tool, matching the
// primary loop's resolution semantics.
func TestAgentRegistry_SchemaModeConfigFullRestoresLegacy(t *testing.T) {
	r := newTestRegistryWithTaskTools(t)
	r.SetSchemaModeConfig(config.AgentToolsConfig{SchemaMode: "full"})

	loop, err := r.GetForTask("coder", "task-schema-full")
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}

	defs := loopRegistryDefinitions(t, loop)
	extra := findTaskSchemaDef(t, defs, "task_schema_extra")
	if len(extra.Function.Parameters.Properties) == 0 {
		t.Fatal(`schema_mode "full" must restore complete parameter schemas for all tools`)
	}
	if len(extra.Function.Parameters.Required) == 0 {
		t.Fatal(`schema_mode "full" must preserve the required list`)
	}
}

// TestAgentRegistry_SchemaModeBeforeLoopCreation verifies the config is
// honored for loops created after SetSchemaModeConfig (createLoop consults
// the stored config even when no GetForTask happened first).
func TestAgentRegistry_SchemaModeBeforeLoopCreation(t *testing.T) {
	r := newTestRegistryWithTaskTools(t)
	r.SetSchemaModeConfig(config.AgentToolsConfig{SchemaMode: "indexed"})

	loop, err := r.GetForTask("coder", "task-schema-order")
	if err != nil {
		t.Fatalf("GetForTask: %v", err)
	}

	defs := loopRegistryDefinitions(t, loop)
	extra := findTaskSchemaDef(t, defs, "task_schema_extra")
	if len(extra.Function.Parameters.Properties) != 0 {
		t.Errorf("loop created after SetSchemaModeConfig must ship a stubbed schema; got %d properties",
			len(extra.Function.Parameters.Properties))
	}
}

// TestAgentRegistry_PlaceholderRegistrySetSchemaModeSafe proves that a
// placeholder tool registry lacking SetSchemaMode does not panic when the
// registry's schema-mode config is applied (interface-assertion guard).
type placeholderToolRegistry struct{}

func (placeholderToolRegistry) Get(name string) tools.Tool           { return nil }
func (placeholderToolRegistry) List() []tools.Tool                   { return nil }
func (placeholderToolRegistry) GetDefinitions() []llm.ToolDefinition { return nil }

// TestAgentRegistry_PlaceholderRegistrySetSchemaModeSafe exercises the
// interface assertion in applySchemaModeLocked with a registry that does not
// implement SetSchemaMode — it must not panic and must leave the registry
// untouched.
func TestAgentRegistry_PlaceholderRegistrySetSchemaModeSafe(t *testing.T) {
	r := newTestRegistryWithTaskTools(t)
	r.tools = placeholderToolRegistry{}
	r.SetSchemaModeConfig(config.AgentToolsConfig{SchemaMode: "full"})

	// No panic above is the assertion; GetForTask must still succeed and
	// the placeholder registry must be left untouched.
	if _, err := r.GetForTask("coder", "task-schema-placeholder"); err != nil {
		t.Fatalf("GetForTask with placeholder registry: %v", err)
	}
}

// loopRegistryDefinitions fetches the tool registry attached to a loop built
// by GetForTask and returns its LLM definitions. Loops do not expose their
// registry directly, so this helper reaches through the package-private
// field (tests live in the same package).
func loopRegistryDefinitions(t *testing.T, loop *AgentLoop) []llm.ToolDefinition {
	t.Helper()
	if loop == nil {
		t.Fatal("loop is nil")
	}
	loop.mu.RLock()
	reg := loop.registry
	loop.mu.RUnlock()
	if reg == nil {
		t.Fatal("loop has no tool registry attached")
	}
	return reg.GetDefinitions()
}
