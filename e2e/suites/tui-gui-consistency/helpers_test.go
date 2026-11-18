//go:build e2e

// helpers_test.go: suite-local primitives for the tui-gui-consistency e2e
// suite (plan tree tui-gui-consistency-fixes leaf 00T). RPC helpers mirror
// the kanban suite's local copies (the harness package is sibling-shared and
// deliberately not extended). The fixture helpers seed a bot + an employee
// into the daemon's own bots.db through the daemon's agents.create RPC and
// the bot store so the roster has all three source types available — no
// hand-replicated roster fixture: the handler's own merge is what is under
// test.
package tuguiconsistency

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/pkg/id"
)

// rpcCall sends one JSON-RPC request over the sandbox Unix socket and
// decodes the full response envelope. Transport errors fail the test;
// application-level errors are returned for the caller to assert on.
func rpcCall(t *testing.T, socketPath, method string, params any) map[string]any {
	t.Helper()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial %s: %v", socketPath, err)
	}
	defer conn.Close()

	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err := fmt.Fprintf(conn, "%d\n", len(payload)); err != nil {
		t.Fatalf("write length: %v", err)
	}
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	reader := bufio.NewReader(conn)
	lengthLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read length line for %s: %v", method, err)
	}
	var length int
	if _, err := fmt.Sscanf(strings.TrimSpace(lengthLine), "%d", &length); err != nil || length <= 0 {
		t.Fatalf("%s: bad length line %q", method, lengthLine)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(reader, buf); err != nil {
		t.Fatalf("%s: read payload: %v", method, err)
	}
	var resp map[string]any
	if err := json.Unmarshal(buf, &resp); err != nil {
		t.Fatalf("%s: decode response %q: %v", method, buf, err)
	}
	return resp
}

// rpcResult asserts the call succeeded (no error object) and returns the
// result object.
func rpcResult(t *testing.T, s *harness.Stack, method string, params any) map[string]any {
	t.Helper()
	resp := rpcCall(t, s.SocketPath, method, params)
	if e, ok := resp["error"]; ok && e != nil {
		t.Fatalf("%s: RPC error: %v", method, e)
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: result is not an object: %v", method, resp["result"])
	}
	return result
}

// str fetches a string field from a decoded RPC result map.
func str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// num fetches a numeric (JSON float64) field from a decoded RPC result map.
func num(m map[string]any, key string) float64 {
	v, _ := m[key].(float64)
	return v
}

// seedSources registers one employee through the daemon's own agents.create
// RPC and one plain bot directly into the daemon's bots.db. The employee is a
// user-facing hire; bot.Manager.ListBots (which the roster merges) sees BOTH
// rows, so the employee row is also visible to the bot source — but the
// roster's bot/employee split distinguishes them by the constitution marker
// the implementer stamps (leaf 01-backend/01). What matters for this suite is
// that all three SOURCE types are genuinely live when agents.roster runs.
func seedSources(t *testing.T, s *harness.Stack) {
	t.Helper()
	suffix := id.Generate("tui-gui-")
	employee := suffix + "-employee"
	botID := suffix + "-bot"

	// Hire a real employee via the daemon's agents.create RPC (the same
	// seeding path e2e/suites/goals-plans uses). The constitution shape is the
	// frozen employee contract; an enabled=false cron trigger is inert.
	created := rpcResult(t, s, "agents.create", map[string]any{
		"id":      employee,
		"name":    employee,
		"prompt":  "You are " + employee + ", an e2e roster probe.",
		"enabled": true,
		"triggers": []map[string]any{
			{"type": "cron", "schedule": "0 4 * * *", "enabled": false},
		},
		"constitution": map[string]any{
			"purpose":       employee,
			"role":          employee,
			"charter":       "Answer briefly.",
			"autonomy_tier": "tier_1_reactive",
			"escalates_to":  []string{"user"},
			"constraints": map[string]any{
				"tools_allowed": []string{},
				"risk_ceiling":  "medium",
			},
			"amendment_policy": map[string]any{
				"self_propose_allowed": false,
				"requires_approval":    true,
				"frozen_fields":        []string{},
			},
			"version":     1,
			"authored_by": "user",
		},
	})
	if got := str(created, "id"); got != employee {
		t.Fatalf("agents.create id = %v, want %s", str(created, "id"), employee)
	}

	// Write a plain bot row straight to the daemon's bots.db so the bot
	// source has a non-employee row to surface. cfg.Bots.DataDir defaults
	// to "~/.meept/bots" (s.MeeptHome/bots) — the harness config does not
	// override it — and the daemon's BotManager owns the same file.
	botDir := s.MeeptHome + "/bots"
	if err := os.MkdirAll(botDir, 0o755); err != nil {
		t.Fatalf("mkdir bot dir %s: %v", botDir, err)
	}
	dbPath := botDir + "/bots.db"
	// busy_timeout so a brief write here never races a daemon read on the
	// same sqlite file (modernc driver honours the _pragma DSN key).
	store, err := bot.NewStore(dbPath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open bots.db %s: %v", dbPath, err)
	}
	defer store.Close()
	now := time.Now().UTC()
	if err := store.Create(context.Background(), bot.BotDefinition{
		ID:        botID,
		Name:      botID,
		Prompt:    "You are " + botID + ", an e2e roster probe.",
		Enabled:   true,
		Triggers:  []bot.BotTrigger{{Type: bot.TriggerTypeCron, Schedule: "0 4 * * *", Enabled: false}},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed bot %s: %v", botID, err)
	}
	// Give the bot a nonzero daily cost so the roster's daily_cost_cents
	// field has a real live value to surface (leaf 01-backend/02).
	if err := store.UpdateState(context.Background(), bot.BotState{
		DefinitionID:   botID,
		Status:         bot.BotStatusPaused,
		TodayCostCents: 123,
	}); err != nil {
		t.Fatalf("seed bot state %s: %v", botID, err)
	}
}
