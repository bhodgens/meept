package tui

import (
	"strings"
	"testing"
	"time"
)

// ---------- countdown formatting (parity contract with leaf 09) ----------

// TestFormatDuration mirrors internal/llm errors_quota.go semantics: truncate
// to the minute, "Nh Mm" when minutes > 0, "Nh" otherwise, "Mm" under an
// hour. There is no seconds tier — a 90s wait shows as "1m" (never rounded
// up) and a 30s wait as "0m".
func TestFormatDuration(t *testing.T) {
	tests := []struct {
		in     time.Duration
		expect string
	}{
		{3*time.Hour + 12*time.Minute, "3h 12m"},
		{3 * time.Hour, "3h"},
		{24 * time.Hour, "24h"},
		{1*time.Hour + 5*time.Minute, "1h 5m"},
		{45 * time.Minute, "45m"},
		{1 * time.Minute, "1m"},
		{59 * time.Minute, "59m"},
		{90 * time.Second, "1m"}, // truncated, not rounded
		{30 * time.Second, "0m"},
		{0, "0m"},
	}
	for _, tt := range tests {
		got := FormatDuration(tt.in)
		if got != tt.expect {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.in, got, tt.expect)
		}
	}
}

func TestQuotaCountdownText(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		unblockAt time.Time
		expect    string
	}{
		{"3h12m", now.Add(3*time.Hour + 12*time.Minute), "quota resets in 3h 12m"},
		{"single hour", now.Add(time.Hour), "quota resets in 1h"},
		{"45m", now.Add(45 * time.Minute), "quota resets in 45m"},
		{"1m", now.Add(time.Minute), "quota resets in 1m"},
		{"past due", now.Add(-time.Hour), "resets soon"},
		{"zero", time.Time{}, "resets soon"},
	}
	for _, tt := range tests {
		if got := QuotaCountdownTextAt(now, tt.unblockAt); got != tt.expect {
			t.Errorf("%s: QuotaCountdownTextAt = %q, want %q", tt.name, got, tt.expect)
		}
	}
}

func TestFormatQuotaCountdown(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name      string
		unblockAt time.Time
		expect    string
	}{
		{"3h12m", now.Add(3*time.Hour + 12*time.Minute), "3h 12m"},
		{"45m", now.Add(45 * time.Minute), "45m"},
		{"1h", now.Add(time.Hour), "1h"},
		{"past due", now.Add(-time.Minute), "soon"},
	}
	for _, tt := range tests {
		if got := FormatQuotaCountdownAt(now, tt.unblockAt); got != tt.expect {
			t.Errorf("%s: FormatQuotaCountdownAt = %q, want %q", tt.name, got, tt.expect)
		}
	}
}

// ---------- badges ----------

func TestQuotaStatus_Badges(t *testing.T) {
	p := &AgentsPanel{}

	// Blocked: error-tone label with the action-required hint.
	if got := p.quotaStatusBadge(nil, true); got != "blocked · action required" {
		t.Errorf("blocked badge = %q, want %q", got, "blocked · action required")
	}

	// Quota wait with a future unblock time (+30s buffer so sub-second
	// wall-clock drift between constructing the time and rendering can't
	// push the truncated minute below 3h12m).
	future := time.Now().Add(3*time.Hour + 12*time.Minute + 30*time.Second)
	got := p.quotaStatusBadge(&future, false)
	if !strings.Contains(got, "quota wait") {
		t.Errorf("quota wait badge %q missing %q", got, "quota wait")
	}
	if !strings.Contains(got, "quota resets in 3h 12m") {
		t.Errorf("quota wait badge %q missing countdown", got)
	}

	// Past-due wait: countdown resolves to "resets soon".
	past := time.Now().Add(-1 * time.Hour)
	got = p.quotaStatusBadge(&past, false)
	if !strings.Contains(got, "quota wait") || !strings.Contains(got, "resets soon") {
		t.Errorf("past-due badge = %q, want quota wait + resets soon", got)
	}

	// Blocked wins over wait time.
	got = p.quotaStatusBadge(&future, true)
	if got != "blocked · action required" {
		t.Errorf("blocked+wait badge = %q, want blocked label", got)
	}
}

// TestQuotaStatus_NoEpisode verifies that agents without quota state render
// byte-identically to before (regression safety).
func TestQuotaStatus_NoEpisode(t *testing.T) {
	p := &AgentsPanel{}
	if got := p.quotaStatusBadge(nil, false); got != "" {
		t.Errorf("expected empty badge when no quota state, got %q", got)
	}
}

func TestAgentStatusBadge(t *testing.T) {
	p := &AgentsPanel{}

	got := p.statusBadge(AgentStateQuotaWait)
	if !strings.Contains(got, "quota wait") {
		t.Errorf("statusBadge(quota_wait) = %q, want it to contain %q", got, "quota wait")
	}

	got = p.statusBadge(AgentStateBlocked)
	if !strings.Contains(got, "blocked · action required") {
		t.Errorf("statusBadge(blocked) = %q, want it to contain %q", got, "blocked · action required")
	}

	// Unaffected states keep their existing color but unchanged text: strip
	// ANSI escapes and compare the visible string.
	if got := stripANSI(p.statusBadge("running")); got != "running" {
		t.Errorf("statusBadge(running) = %q, want visible %q", got, "running")
	}
	if got := stripANSI(p.statusBadge("paused")); got != "paused" {
		t.Errorf("statusBadge(paused) = %q, want visible %q", got, "paused")
	}
	if got := stripANSI(p.statusBadge("error")); got != "error" {
		t.Errorf("statusBadge(error) = %q, want visible %q", got, "error")
	}
}

// stripANSI removes ANSI escape sequences so tests assert on visible text.
func stripANSI(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEscape = true
		case inEscape && r == 'm':
			inEscape = false
		case !inEscape:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ---------- detail-view primary/active model lines ----------

func TestRenderQuotaDetailLines(t *testing.T) {
	until := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	lines := RenderQuotaDetailLines("claude-opus-4", "glm-4.7", until)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %v", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "primary: claude-opus-4 (blocked until ") {
		t.Errorf("line 0 = %q, want primary model with blocked-until", lines[0])
	}
	if !strings.Contains(lines[0], "12:00") {
		t.Errorf("line 0 = %q, want formatted time", lines[0])
	}
	if lines[1] != "active: glm-4.7" {
		t.Errorf("line 1 = %q, want %q", lines[1], "active: glm-4.7")
	}

	// No fallback -> no extra lines (agents never quota-hit unaffected).
	if got := RenderQuotaDetailLines("claude-opus-4", "", until); got != nil {
		t.Errorf("no-fallback lines = %v, want nil", got)
	}

	// Missing unblock time degrades gracefully.
	lines = RenderQuotaDetailLines("claude-opus-4", "glm-4.7", time.Time{})
	if len(lines) != 2 || !strings.Contains(lines[0], "(blocked until unknown)") {
		t.Errorf("zero-time lines = %v, want blocked until unknown", lines)
	}
}

// ---------- live event handling ----------

func TestQuotaStateMsgTransitions(t *testing.T) {
	p := NewAgentsPanel(nil)
	unblock := time.Now().Add(3*time.Hour + 12*time.Minute)

	// quota_wait transition sets episode state and the row status.
	p.agents = []AgentSummary{
		{ID: "agent-1", Status: "running"},
	}
	p.Update(quotaStateMsg{
		agentID:       "agent-1",
		to:            AgentStateQuotaWait,
		waitUntil:     &unblock,
		model:         "claude-opus-4",
		fallbackModel: "glm-4.7",
	})
	a := p.agents[0]
	if a.Status != AgentStateQuotaWait {
		t.Errorf("status = %q, want %q", a.Status, AgentStateQuotaWait)
	}
	if a.QuotaWaitUntil == nil || !a.QuotaWaitUntil.Equal(unblock) {
		t.Errorf("QuotaWaitUntil = %v, want %v", a.QuotaWaitUntil, unblock)
	}
	if a.QuotaModel != "claude-opus-4" || a.QuotaFallbackModel != "glm-4.7" {
		t.Errorf("models = (%q, %q), want (claude-opus-4, glm-4.7)", a.QuotaModel, a.QuotaFallbackModel)
	}

	// blocked transition marks the hard stop.
	p.Update(quotaStateMsg{
		agentID:   "agent-1",
		to:        AgentStateBlocked,
		waitUntil: &unblock,
	})
	if got := p.agents[0]; !got.QuotaBlocked || got.Status != AgentStateBlocked {
		t.Errorf("after blocked: %+v, want blocked status", got)
	}

	// quota_cleared (running) drops all episode data.
	p.Update(quotaStateMsg{agentID: "agent-1", to: "running"})
	a = p.agents[0]
	if a.Status != "running" || a.QuotaWaitUntil != nil || a.QuotaBlocked || a.QuotaFallbackModel != "" {
		t.Errorf("after running: %+v, want episode data cleared", a)
	}

	// Unknown agent id is a no-op.
	p.Update(quotaStateMsg{agentID: "no-such-agent", to: AgentStateQuotaWait, waitUntil: &unblock})
	if p.agents[0].Status != "running" {
		t.Errorf("unknown agent update changed state: %+v", p.agents[0])
	}
}

// TestQuotaCountdownTick verifies the live countdown refresh: the tick is
// armed when an episode becomes active, re-renders the badge text on each
// tick (countdown recomputed from the current time), and stops itself when
// the last episode clears (leaf 08 Task 2: countdown updates on the tick).
func TestQuotaCountdownTick(t *testing.T) {
	p := NewAgentsPanel(nil)
	p.agents = []AgentSummary{{ID: "agent-1", Status: "running"}}

	// No episode -> quotaStateMsg arms nothing.
	if cmd := p.Update(quotaStateMsg{agentID: "agent-1", to: "running"}); cmd != nil {
		t.Fatalf("expected nil cmd with no episode, got non-nil")
	}

	// Entering quota_wait arms the tick.
	unblock := time.Now().Add(45 * time.Minute)
	if cmd := p.Update(quotaStateMsg{agentID: "agent-1", to: AgentStateQuotaWait, waitUntil: &unblock}); cmd == nil {
		t.Fatal("expected tick cmd when episode becomes active, got nil")
	}
	p.countdownTick = true // simulate the armed tick's in-flight window

	// The tick message re-renders and re-arms while the episode is live.
	if cmd := p.Update(quotaCountdownTickMsg{}); cmd == nil {
		t.Fatal("expected tick to re-arm while an episode is active")
	}

	// Clearing the episode stops the tick: the next tick message returns
	// nil and drops the armed flag.
	p.Update(quotaStateMsg{agentID: "agent-1", to: "running"})
	if p.countdownTick {
		t.Fatal("expected countdownTick reset after last episode cleared")
	}
	if cmd := p.Update(quotaCountdownTickMsg{}); cmd != nil {
		t.Fatal("expected tick to stop after last episode cleared")
	}

	// Re-render recomputes the countdown text from the current time: build
	// the badge directly and confirm the formatter output is live (the
	// cached cell is rebuilt, not frozen at event time).
	future := time.Now().Add(3*time.Hour + 12*time.Minute)
	p.agents[0].QuotaWaitUntil = &future
	p.updateAgentsTable()
	cell := p.table.Rows()[0][1]
	if !strings.Contains(stripANSI(cell), "quota resets in 3h 12m") {
		t.Errorf("rebuilt cell = %q, want live countdown", cell)
	}
}

// TestQuotaStateMsgTransitions verifies that to == "" events
// (12h warn / 20h action_recommended tier firings, leaf 05 contract) refresh
// the live episode instead of clearing it — parity with the Flutter provider
// (agent_provider.dart handleQuotaEvent case '').
func TestQuotaStateMsg_TierEscalationRefresh(t *testing.T) {
	p := NewAgentsPanel(nil)
	unblock := time.Now().Add(3 * time.Hour)
	extended := unblock.Add(12 * time.Hour)

	// Live episode first.
	p.agents = []AgentSummary{{ID: "agent-1", Status: "running"}}
	p.Update(quotaStateMsg{
		agentID:   "agent-1",
		to:        AgentStateQuotaWait,
		waitUntil: &unblock,
		model:     "claude-opus-4",
	})

	// Tier escalation (to == "", escalation == "warn", extended unblock):
	// episode persists, unblock time refreshes, status stays quota_wait.
	p.Update(quotaStateMsg{
		agentID:    "agent-1",
		to:         "",
		waitUntil:  &extended,
		model:      "claude-opus-4",
		escalation: "warn",
	})
	a := p.agents[0]
	if a.Status != AgentStateQuotaWait {
		t.Errorf("after tier event: status = %q, want %q (episode must persist)", a.Status, AgentStateQuotaWait)
	}
	if a.QuotaWaitUntil == nil || !a.QuotaWaitUntil.Equal(extended) {
		t.Errorf("after tier event: QuotaWaitUntil = %v, want %v", a.QuotaWaitUntil, extended)
	}
	if a.QuotaModel != "claude-opus-4" {
		t.Errorf("after tier event: model = %q, want it kept", a.QuotaModel)
	}

	// A bare to == "" event (escalation == "" AND no unblock time) is a
	// genuine clear: episode data is dropped.
	p.Update(quotaStateMsg{agentID: "agent-1", to: ""})
	a = p.agents[0]
	if a.Status != "running" || a.QuotaWaitUntil != nil || a.QuotaBlocked || a.QuotaFallbackModel != "" {
		t.Errorf("after bare to==\"\": %+v, want episode data cleared", a)
	}
}

// ---------- view-model defaults ----------

func TestAgentSummary_QuotaFields(t *testing.T) {
	a := AgentSummary{
		ID:             "agent-1",
		Name:           "test-agent",
		Role:           "coder",
		Status:         "running",
		Tier:           "tier_1_reactive",
		DriftScore:     0.01,
		DailyCostCents: 50,
		FindingsCount:  0,
	}
	if a.QuotaWaitUntil != nil {
		t.Error("expected QuotaWaitUntil to be nil by default")
	}
	if a.QuotaBlocked {
		t.Error("expected QuotaBlocked to be false by default")
	}
	if a.QuotaModel != "" || a.QuotaFallbackModel != "" {
		t.Error("expected quota model fields empty by default")
	}
}
