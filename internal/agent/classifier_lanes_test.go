package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// The gold corpora pin the lanes the LLM classifier must be able to emit.
// That lane list used to live in three places - the classifier prompt, the
// multi-intent prompt, and the isValidIntent gate - and they drifted.
// IntentQuickPlan shipped with a dispatcher route, an agent mapping and 58
// corpus cases while being absent from all three, so the classifier could
// never emit it and "just do it" prompts landed on a wrong lane (2026-09-12:
// a tool-invocation prompt classified as platform at 0.9). These guards fail
// whenever a corpus lane is unreachable again.
var goldCorpora = []string{
	"../../testdata/eval/classifier-test-corpus.json5",
	"../../testdata/eval/smoke-test-corpus.json5",
	"../../testdata/eval/classifier-adversarial-corpus.json5",
}

var (
	reExpectedIntent = regexp.MustCompile(`expected_intent:\s*"([a-z_]+)"`)
	// reBareIntent matches "intent: \"x\"" but not the "expected_intent" key
	// (the [^_] excludes the underscore that precedes it there).
	reBareIntent    = regexp.MustCompile(`[^_]intent:\s*"([a-z_]+)"`)
	reExpectedAgent = regexp.MustCompile(`expected_intent:\s*"([a-z_]+)"\s*,\s*expected_agent:\s*"([a-z_]+)"`)
)

func TestClassifierLanes_CoverGoldCorpora(t *testing.T) {
	for _, path := range goldCorpora {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(data)
		checked := 0
		for _, m := range reExpectedIntent.FindAllStringSubmatch(body, -1) {
			checked++
			if !isValidIntent(m[1]) {
				t.Errorf("%s: expected_intent %q is not a classifier lane", path, m[1])
			}
		}
		for _, m := range reBareIntent.FindAllStringSubmatch(body, -1) {
			checked++
			if !isValidIntent(m[1]) {
				t.Errorf("%s: intent %q is not a classifier lane", path, m[1])
			}
		}
		if checked == 0 {
			t.Errorf("%s: no intent labels found, so this guard checked nothing", path)
		}
	}
}

func TestClassifierLanes_AgentMappingMatchesCorpus(t *testing.T) {
	for _, path := range goldCorpora {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		pairs := reExpectedAgent.FindAllStringSubmatch(string(data), -1)
		for _, p := range pairs {
			if got := agentForIntent(p[1]); got != p[2] {
				t.Errorf("%s: agentForIntent(%q) = %q, want the corpus agent %q", path, p[1], got, p[2])
			}
		}
	}
}

// Every advertised lane must be emittable AND routable. A lane that appears
// in the prompt but fails isValidIntent or has no agent is a trap: the model
// picks it and the dispatcher drops it.
func TestClassifierLanes_AllAdvertisedLanesAreRoutable(t *testing.T) {
	list := laneList()
	for _, lane := range classifierLanes {
		name := string(lane)
		if !strings.Contains(list, name) {
			t.Errorf("laneList() omits %q", name)
		}
		if !isValidIntent(name) {
			t.Errorf("isValidIntent(%q) = false, but the prompt advertises it", name)
		}
		if agent := agentForIntent(name); agent == "" {
			t.Errorf("agentForIntent(%q) is empty; the lane has no route", name)
		}
	}
	if len(classifierLanes) == 0 {
		t.Fatal("classifierLanes is empty")
	}
}

// The lanes this campaign added to the system must be reachable through the
// LLM classifier, not only through the capability matcher.
func TestClassifierLanes_CampaignLanesReachable(t *testing.T) {
	cases := []struct {
		lane  string
		agent string
	}{
		{"quickplan", "orchestrator"},
		{"research", "researcher"},
		{"tooluse", "coder"},
	}
	for _, tc := range cases {
		if !isValidIntent(tc.lane) {
			t.Errorf("isValidIntent(%q) = false; the LLM classifier cannot emit it", tc.lane)
		}
		if got := agentForIntent(tc.lane); got != tc.agent {
			t.Errorf("agentForIntent(%q) = %q, want %q", tc.lane, got, tc.agent)
		}
	}
}

// A lane without a description renders as "Unknown intent" in the prompt, which
// tells the model the option exists but not what it means.
func TestClassifierLanes_EveryLaneHasADescription(t *testing.T) {
	c := &LLMClassifier{}
	for _, lane := range classifierLanes {
		desc := c.getIntentDescription(string(lane))
		if desc == "Unknown intent" || strings.TrimSpace(desc) == "" {
			t.Errorf("lane %q has no description; the prompt would render it blank", lane)
		}
	}
}

// The user-message prompt is a second lane list (separate from the system
// prompt). It was hard-coded to the original 12 lanes and silently omitted
// quickplan, research and the rest - the model reads THIS list, so a lane
// missing here is unreachable no matter what the system prompt says.
func TestBuildClassificationPrompt_ListsEveryLane(t *testing.T) {
	c := &LLMClassifier{}
	prompt := c.buildClassificationPrompt("test input")
	for _, lane := range classifierLanes {
		if !strings.Contains(prompt, "- "+string(lane)+":") {
			t.Errorf("classification prompt omits lane %q", lane)
		}
	}
}

// writeTempAgentDefinition writes a minimal AGENT.md under
// <dir>/<id>/AGENT.md with the given `intents:` flow-list body and returns
// the base directory. Used to prove a brand-new agent becomes routable with a
// frontmatter edit alone (no Go change).
func writeTempAgentDefinition(t *testing.T, dir, id string, intents []string) {
	t.Helper()
	agentDir := filepath.Join(dir, id)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", agentDir, err)
	}
	body := "---\n" +
		"id: " + id + "\n" +
		"name: Test Specialist\n" +
		"role: executor\n" +
		"enabled: true\n" +
		"intents: [" + strings.Join(intents, ", ") + "]\n" +
		"---\n\n# Test Specialist\n\nBrand new agent added with no Go change.\n"
	path := filepath.Join(agentDir, "AGENT.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestAgentForIntent_NewAgentRoutableFromFrontmatter is the core contract of
// the frontmatter-driven routing: a brand-new AGENT.md declaring
// `intents: [quickplan]` becomes the destination for that lane, purely from
// the definition file. No Go table is edited.
func TestAgentForIntent_NewAgentRoutableFromFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writeTempAgentDefinition(t, dir, "test-specialist", []string{"quickplan"})

	idx, err := BuildLaneAgentIndexFromDir(dir)
	if err != nil {
		t.Fatalf("BuildLaneAgentIndexFromDir: %v", err)
	}
	if got := idx["quickplan"]; got != "test-specialist" {
		t.Fatalf("index[quickplan] = %q, want test-specialist", got)
	}

	prev := CurrentLaneAgentIndex()
	PublishLaneAgentIndex(idx)
	t.Cleanup(func() { PublishLaneAgentIndex(prev) })

	if got := agentForIntent("quickplan"); got != "test-specialist" {
		t.Errorf("agentForIntent(quickplan) = %q, want test-specialist (frontmatter-declared, no Go change)", got)
	}
}

// TestAgentForIntent_UndeclaredLaneFallsBackToStaticTable proves the static
// fallback still covers lanes no agent declares: with an index that declares
// only quickplan, a lane absent from the index (git) resolves through
// agentMapping, and a lane absent from agentMapping (write) resolves through
// IntentType.DefaultAgent. Existing behavior never regresses.
func TestAgentForIntent_UndeclaredLaneFallsBackToStaticTable(t *testing.T) {
	dir := t.TempDir()
	writeTempAgentDefinition(t, dir, "test-specialist", []string{"quickplan"})

	idx, err := BuildLaneAgentIndexFromDir(dir)
	if err != nil {
		t.Fatalf("BuildLaneAgentIndexFromDir: %v", err)
	}
	if _, declared := idx["git"]; declared {
		t.Fatalf("test setup: git should be undeclared in the temp index, got %q", idx["git"])
	}

	prev := CurrentLaneAgentIndex()
	PublishLaneAgentIndex(idx)
	t.Cleanup(func() { PublishLaneAgentIndex(prev) })

	// git is not declared by any agent -> static agentMapping -> committer.
	if got := agentForIntent("git"); got != config.AgentIDCommitter {
		t.Errorf("agentForIntent(git) = %q, want %q via the static agentMapping fallback", got, config.AgentIDCommitter)
	}
	// write has no agentMapping entry -> IntentType.DefaultAgent -> writer.
	if got := agentForIntent("write"); got != config.AgentIDWriter {
		t.Errorf("agentForIntent(write) = %q, want %q via the DefaultAgent fallback", got, config.AgentIDWriter)
	}
}

// TestLaneAgentIndex_ConfigAgentsMatchStaticRoutes guards the shipped
// config/agents frontmatter: every lane an agent declares must resolve to the
// same agent the static tables produced before `intents:` existed, so the
// dynamic path is primary without changing routing for existing lanes.
func TestLaneAgentIndex_ConfigAgentsMatchStaticRoutes(t *testing.T) {
	idx, err := BuildLaneAgentIndexFromDir("../../config/agents")
	if err != nil {
		t.Fatalf("BuildLaneAgentIndexFromDir(config/agents): %v", err)
	}
	if len(idx) == 0 {
		t.Fatal("config/agents declares no intents; the dynamic routing path is not primary")
	}

	// Lanes that must be frontmatter-declared so the dynamic path is primary
	// rather than a fallback.
	mustDeclare := []string{
		"code", "debug", "plan", "git", "schedule", "research",
		"write", "architect", "skeptic", "librarian", "explore",
		"image_gen", "image_id", "video_gen", "analyze", "search",
		"chat", "report",
	}
	for _, lane := range mustDeclare {
		if _, ok := idx[lane]; !ok {
			t.Errorf("config/agents declares no agent for lane %q; the dynamic routing path is not primary", lane)
		}
	}

	for lane, declared := range idx {
		var static string
		if a, ok := agentMapping[lane]; ok && a != "" {
			static = a
		} else {
			static = IntentType(lane).DefaultAgent()
		}
		if declared != static {
			t.Errorf("lane %q: frontmatter routes to %q but the static tables route to %q; existing behavior would change", lane, declared, static)
		}
	}
}
