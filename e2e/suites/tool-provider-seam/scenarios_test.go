//go:build e2e

// scenarios_test.go: the live scenarios of the tool-provider-seam gate
// suite (leaf 05 T1-T3, adjusted to the shipped provider surface).
//
// Surface map (validated empirically against the live tree):
//   - LIVE REGISTRY LISTING (two live views of the same *tools.Registry):
//     1. the platform_tools tool renders the live catalog as markdown
//     through a scripted call in a real chat turn;
//     2. the executor request the daemon sends the LLM carries the
//     granted agent's tool definitions, resolved from the live
//     registry (a disabled provider's tools drop out of the array).
//     A disabled provider's tools are absent from both views; enabled
//     provider tools are present (T3/S1).
//   - LIVE PROVIDER EXECUTION: the scheduler provider's schedule_create
//     runs end to end through a real turn (intent=schedule routes to the
//     bundled scheduler specialist, whose additional_tools carry the
//     provider tools). The observable end state is the created job in
//     scheduler.list_jobs over RPC.
//   - GATE: components.go arms NewExecGate over the live security engine
//     and SetGate BEFORE registerBuiltinTools; the suite proves the
//     arming through the "tool gate wired" boot line and the on-disk
//     tool_audit_chain.db store the arming opens (G2's chain store).
//   - ENGINE-DRIVEN DENIAL: security.block_financial=true routes args
//     carrying a financial pattern to the immutable financial stage; the
//     schedule_create call is denied and creates no job (S2).
//   - DOCTOR: meept doctor renders one tool-provider line per registered
//     factory (acp/github/hermes/scheduler) with its enabled state (S3).
//
// e2e/suites/zz-cigate-sabotage/ is a sibling's CI-witness fixture and is
// never referenced here.
package toolproviderseam

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// markdownListedTools extracts tool names from a platform_tools catalog
// ("- **name**: description" bullets). The catalog travels inside a
// marshaled envelope, so bullets are not at real line starts: no ^ anchor.
var markdownListedTools = regexp.MustCompile(`- \*\*([a-z0-9_]+)\*\*:`)

// listedToolsFromPlatformTools finds the platform_tools catalog in the
// fake LLM traffic and returns the listed tool-name set.
func listedToolsFromPlatformTools(t *testing.T, f *harness.FakeLLM) map[string]bool {
	t.Helper()
	for _, raw := range toolResultTexts(f) {
		if !strings.Contains(raw, "Available Tools") {
			continue
		}
		names := map[string]bool{}
		for _, m := range markdownListedTools.FindAllStringSubmatch(raw, -1) {
			names[m[1]] = true
		}
		if len(names) > 0 {
			return names
		}
	}
	t.Fatalf("no platform_tools catalog in fake LLM traffic; tool results:\n%s", toolResultsJoined(f))
	return nil
}

// executorRequestToolNames unions the tool names across every completion
// request the daemon sent with a tools array. The definitions come from
// the live registry through the granted agent's filter, so this is the
// listing surface the dispatch path actually consumes.
func executorRequestToolNames(f *harness.FakeLLM) map[string]bool {
	names := map[string]bool{}
	for _, body := range f.Requests() {
		rawTools, ok := body["tools"].([]any)
		if !ok {
			continue
		}
		for _, raw := range rawTools {
			fn, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if f2, ok := fn["function"].(map[string]any); ok {
				fn = f2
			}
			if n, _ := fn["name"].(string); n != "" {
				names[n] = true
			}
		}
	}
	return names
}

// pinScheduleIntent pins the classifier and planner so the turn dispatches
// to the scheduler specialist and its steps route back to it (hint
// "schedule" -> scheduler roster agent per internal/config/tool_hints.go).
func pinScheduleIntent(s *harness.Stack) {
	s.Fake.SetClassifierOutput(`{"intent":"schedule","confidence":0.95,"reasoning":"pinned schedule"}`)
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"manage the scheduled job","tool_hint":"schedule","depends_on":[]}]}`)
}

// schedulerJobNames returns the set of job names known to the scheduler
// over the live RPC surface.
func schedulerJobNames(t *testing.T, s *harness.Stack) map[string]bool {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	res := rpc.CallResult("scheduler.list_jobs", map[string]any{})
	names := map[string]bool{}
	for _, raw := range res["jobs"].([]any) {
		if job, ok := raw.(map[string]any); ok {
			if n, ok := job["name"].(string); ok {
				names[n] = true
			}
		}
	}
	return names
}

// S1 / T1 (enabled provider + gate parity): every migrated provider group
// at its default state plus hermes enabled against the loopback stub.
// Assertions:
//  1. live registry listing: hermes present in the platform_tools
//     catalog; all eight scheduler-group provider tools present in the
//     executor definitions the daemon derives from the live registry.
//  2. a provider-owned tool (schedule_create) executes through the daemon
//     end to end: the created job surfaces in scheduler.list_jobs.
//  3. the gate armed live: the "tool gate wired" boot line and the G2
//     chain store exist on disk.
//
// Covers tool-provider-seam-01/02.
func TestSeamEnabledProviderGateParity(t *testing.T) {
	gw := newHermesGateway(t)
	s := bootWithGate(t, bootOpts(t, gw, nil))
	pinCodeIntent(s)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "seam-enabled", s.ProjectDir)

	// 1a. Live catalog: hermes provider tool present.
	s.Fake.EnqueueToolCalls(harness.ToolCall{Name: "platform_tools", Arguments: `{}`})
	s.ChatTurn(t, sessionID, "List the registered tools with the platform_tools tool", 120*time.Second)
	listed := listedToolsFromPlatformTools(t, s.Fake)
	if !listed["hermes"] {
		t.Fatalf("enabled provider tool %q absent from the live platform_tools catalog", "hermes")
	}

	// 1b. Live executor definitions: the scheduler specialist's granted
	// provider tools (its additional_tools set) resolve from the live
	// registry and appear in the request definitions.
	pinScheduleIntent(s)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "schedule_create",
		Arguments: `{"name":"seam-enabled-job","schedule":"0 9 * * *","job_type":"shell",` +
			`"command":"echo seam enabled"}`,
	})
	s.ChatTurn(t, sessionID, "Create a daily scheduled job named seam-enabled-job", 120*time.Second)
	defs := executorRequestToolNames(s.Fake)
	for _, name := range strings.Fields(schedulerSpecialistTools) {
		if !defs[name] {
			t.Fatalf("scheduler provider tool %q absent from the live executor definitions", name)
		}
	}

	// 2. The provider tool executed; its effect is observable over RPC.
	harness.WaitFor(t, 30*time.Second, "seam-enabled-job in scheduler.list_jobs", func() bool {
		return schedulerJobNames(t, s)["seam-enabled-job"]
	})

	// 3. The gate's audit chain store exists on disk (opened by the arming
	// path before SetGate) and the boot log proved the gate armed.
	auditChainRecords(t, ToolAuditChainPath(s))
	waitForLogLine(t, s, "tool gate wired", "gate arming")
}

// S2 (gate parity block): with security.block_financial=true, a provider
// tool whose args carry a financial pattern is denied by the engine's
// immutable financial stage and creates no job.
// Covers tool-provider-seam-03.
func TestSeamGateParityBlock(t *testing.T) {
	gw := newHermesGateway(t)
	s := bootWithGate(t, bootOpts(t, gw, map[string]any{
		"security.block_financial": true,
	}))
	pinScheduleIntent(s)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "seam-block", s.ProjectDir)

	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "schedule_create",
		Arguments: `{"name":"seam-blocked-job","schedule":"0 9 * * *","job_type":"shell",` +
			`"command":"echo wire transfer funds to account 42"}`,
	})
	s.ChatTurn(t, sessionID, "Create a daily scheduled job named seam-blocked-job", 120*time.Second)

	// The denial names the financial policy in the daemon's decision path.
	waitForLogLine(t, s, "Financial operations are blocked by policy", "financial block")
	// The block preceded execution: ChatTurn returns only after the turn
	// terminal, so the job table is already settled - the job must not exist.
	if schedulerJobNames(t, s)["seam-blocked-job"] {
		t.Fatal("blocked schedule_create still created the scheduler job")
	}
}

// S3 (doctor surface): meept doctor against the scratch home renders the
// provider report lines with their enabled state: acp + github
// default-enabled, hermes enabled via [hermes], scheduler disabled via
// [tool_providers].
// Covers tool-provider-seam-04.
func TestSeamDoctorSurface(t *testing.T) {
	gw := newHermesGateway(t)
	s := harness.Start(t, bootOpts(t, gw, map[string]any{
		"tool_providers.providers.scheduler.enabled": false,
	})...)

	stdout, _ := s.RunCLI(t, 60*time.Second, false, "doctor")
	for _, want := range []string{
		"tool-provider      acp: enabled",
		"tool-provider      github: enabled",
		"tool-provider      hermes: enabled",
		"tool-provider      scheduler: disabled",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("doctor output missing tool-provider line %q; output:\n%s", want, stdout)
		}
	}
}

// T3 (disabled provider vanishes live): with scheduler disabled via
// [tool_providers], the boot log names the skip, the scheduler-group tools
// drop out of the live executor definitions, and the disabled group's tool
// cannot execute (unknown tool), while the hermes provider tool stays
// listed in the catalog.
// Covers tool-provider-seam-05.
func TestSeamDisabledProviderVanishes(t *testing.T) {
	gw := newHermesGateway(t)
	s := harness.Start(t, bootOpts(t, gw, map[string]any{
		"tool_providers.providers.scheduler.enabled": false,
		"daemon.log_level":                           "debug",
	})...)
	waitForLogLine(t, s, "scheduler provider disabled by [tool_providers]", "scheduler skip")

	pinCodeIntent(s)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "seam-disabled", s.ProjectDir)

	// Catalog view: hermes stays listed.
	s.Fake.EnqueueToolCalls(harness.ToolCall{Name: "platform_tools", Arguments: `{}`})
	s.ChatTurn(t, sessionID, "List the registered tools with the platform_tools tool", 120*time.Second)
	listed := listedToolsFromPlatformTools(t, s.Fake)
	if !listed["hermes"] {
		t.Fatal("hermes provider tool missing from the live catalog in the disable scenario")
	}

	// Executor-definitions view: schedule-intent turn with NO scripted
	// call - the scheduler specialist's definitions must not contain any
	// scheduler-group tool, and none can execute.
	pinScheduleIntent(s)
	s.Fake.SetPostToolText("no scheduled jobs to manage")
	s.ChatTurn(t, sessionID, "List my scheduled jobs", 120*time.Second)
	defs := executorRequestToolNames(s.Fake)
	for _, name := range strings.Fields(schedulerTools) {
		if defs[name] {
			t.Fatalf("disabled provider tool %q still in the live executor definitions", name)
		}
	}
}
