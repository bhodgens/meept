package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/pkg/models"
)

func createTestModelsConfig() *config.ModelsConfig {
	return &config.ModelsConfig{
		Model: "test/test",
		Providers: map[string]config.Provider{
			"test": {
				API: "openai",
				Options: config.ProviderOptions{
					BaseURL: "http://localhost:9999",
					APIKey:  "test",
					Timeout: 5,
				},
				Models: map[string]config.Model{
					"test": {
						Name:         "test",
						Capabilities: []string{"chat"},
						ContextLimit: 4096,
						Temperature:  0.7,
					},
				},
			},
		},
	}
}

func TestDaemonStartup(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	cfg := &Config{
		SocketPath:      filepath.Join(tmpDir, "meept.sock"),
		PIDFile:         filepath.Join(tmpDir, "meept.pid"),
		StateDir:        tmpDir,
		ShutdownTimeout: 2 * time.Second,
		ModelsConfig:    createTestModelsConfig(),
	}

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	// Start daemon in background
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- d.Run(ctx)
	}()

	// Wait for socket to appear (with timeout)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.SocketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for status to be "running" with retry (race-free check)
	// This ensures we read the status after Run() has set it
	var status models.DaemonStatus
	for range 10 {
		status = d.Status()
		if status == models.StatusRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify daemon is running
	if status != models.StatusRunning {
		t.Errorf("Expected status 'running', got %q", status)
	}

	// Clean shutdown
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Daemon returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Daemon did not shut down in time")
	}
}

func BenchmarkDaemonStartup(b *testing.B) {
	for range b.N {
		tmpDir := b.TempDir()
		cfg := &Config{
			SocketPath:      filepath.Join(tmpDir, "meept.sock"),
			PIDFile:         filepath.Join(tmpDir, "meept.pid"),
			StateDir:        tmpDir,
			ShutdownTimeout: 1 * time.Second,
			ModelsConfig:    createTestModelsConfig(),
		}

		b.StartTimer()
		d, err := New(cfg)
		if err != nil {
			b.Fatalf("Failed to create daemon: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		ready := make(chan struct{})

		go func() {
			// Run daemon in a separate goroutine so socket polling can proceed
			go func() { _ = d.Run(ctx) }()
			// Wait for socket
			for range 100 {
				if _, err := os.Stat(cfg.SocketPath); err == nil {
					close(ready)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}()

		<-ready
		b.StopTimer()

		cancel()
		time.Sleep(100 * time.Millisecond)
	}
}

// BenchmarkRPCThroughput measures RPC requests per second.
func BenchmarkRPCThroughput(b *testing.B) {
	// Setup daemon
	tmpDir := b.TempDir()
	cfg := &Config{
		SocketPath:      filepath.Join(tmpDir, "meept.sock"),
		PIDFile:         filepath.Join(tmpDir, "meept.pid"),
		StateDir:        tmpDir,
		ShutdownTimeout: 2 * time.Second,
		ModelsConfig:    createTestModelsConfig(),
	}

	d, err := New(cfg)
	if err != nil {
		b.Fatalf("Failed to create daemon: %v", err)
	}

	ctx := b.Context()
	go func() { _ = d.Run(ctx) }()

	// Wait for socket
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.SocketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Create connection
	conn, err := net.Dial("unix", cfg.SocketPath)
	if err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Prepare ping request
	pingReq := map[string]any{
		"jsonrpc": "2.0",
		"method":  "ping",
		"id":      1,
	}
	reqData, _ := json.Marshal(pingReq)
	frame := fmt.Sprintf("%d\n%s", len(reqData), reqData)

	b.ResetTimer()
	for range b.N {
		// Send request
		_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err := conn.Write([]byte(frame)); err != nil {
			b.Fatalf("Write failed: %v", err)
		}

		// Read response (simplified - just read some bytes)
		buf := make([]byte, 256)
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := conn.Read(buf); err != nil {
			b.Fatalf("Read failed: %v", err)
		}
	}
}

// BenchmarkConcurrentRPC tests concurrent RPC throughput.
func BenchmarkConcurrentRPC(b *testing.B) {
	// Setup daemon
	tmpDir := b.TempDir()
	cfg := &Config{
		SocketPath:      filepath.Join(tmpDir, "meept.sock"),
		PIDFile:         filepath.Join(tmpDir, "meept.pid"),
		StateDir:        tmpDir,
		ShutdownTimeout: 2 * time.Second,
		ModelsConfig:    createTestModelsConfig(),
	}

	d, err := New(cfg)
	if err != nil {
		b.Fatalf("Failed to create daemon: %v", err)
	}

	ctx := b.Context()
	go func() { _ = d.Run(ctx) }()

	// Wait for socket
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.SocketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Create connection pool
	const poolSize = 10
	conns := make([]net.Conn, poolSize)
	for i := range poolSize {
		conn, err := net.Dial("unix", cfg.SocketPath)
		if err != nil {
			b.Fatalf("Failed to connect: %v", err)
		}
		conns[i] = conn
	}
	defer func() {
		for _, conn := range conns {
			conn.Close()
		}
	}()

	b.ResetTimer()

	var wg sync.WaitGroup
	var ops atomic.Int64

	for i := range poolSize {
		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()

			pingReq := map[string]any{
				"jsonrpc": "2.0",
				"method":  "ping",
				"id":      1,
			}
			reqData, _ := json.Marshal(pingReq)
			frame := fmt.Sprintf("%d\n%s", len(reqData), reqData)
			buf := make([]byte, 256)

			for ops.Load() < int64(b.N) {
				_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
				if _, err := conn.Write([]byte(frame)); err != nil {
					return
				}

				_ = conn.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := conn.Read(buf); err != nil {
					return
				}

				ops.Add(1)
			}
		}(conns[i])
	}

	wg.Wait()
}

// TestRPCLoadTest runs a load test with 1000 concurrent requests.
func TestRPCLoadTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	// Setup daemon
	tmpDir := t.TempDir()
	cfg := &Config{
		SocketPath:      filepath.Join(tmpDir, "meept.sock"),
		PIDFile:         filepath.Join(tmpDir, "meept.pid"),
		StateDir:        tmpDir,
		ShutdownTimeout: 5 * time.Second,
		ModelsConfig:    createTestModelsConfig(),
	}

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create daemon: %v", err)
	}

	ctx := t.Context()
	go func() { _ = d.Run(ctx) }()

	// Wait for socket
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(cfg.SocketPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Run 1000 concurrent requests
	const numRequests = 1000
	const concurrency = 50

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var failCount atomic.Int64

	start := time.Now()

	for i := range concurrency {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			conn, err := net.Dial("unix", cfg.SocketPath)
			if err != nil {
				failCount.Add(int64(numRequests / concurrency))
				return
			}
			defer conn.Close()

			pingReq := map[string]any{
				"jsonrpc": "2.0",
				"method":  "ping",
				"id":      1,
			}
			reqData, _ := json.Marshal(pingReq)
			frame := fmt.Sprintf("%d\n%s", len(reqData), reqData)
			buf := make([]byte, 256)

			for range numRequests / concurrency {
				_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
				if _, err := conn.Write([]byte(frame)); err != nil {
					failCount.Add(1)
					continue
				}

				_ = conn.SetReadDeadline(time.Now().Add(time.Second))
				if _, err := conn.Read(buf); err != nil {
					failCount.Add(1)
					continue
				}

				successCount.Add(1)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	success := successCount.Load()
	fail := failCount.Load()
	rps := float64(success) / duration.Seconds()

	t.Logf("Load Test Results:")
	t.Logf("  Total requests: %d", numRequests)
	t.Logf("  Successful: %d", success)
	t.Logf("  Failed: %d", fail)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Throughput: %.0f req/sec", rps)

	// Target: 2000 req/sec minimum
	const targetRPS = 2000
	if rps < targetRPS {
		t.Errorf("Throughput %.0f req/sec below target %d req/sec", rps, targetRPS)
	} else {
		t.Logf("  PASS: Exceeds %d req/sec target", targetRPS)
	}

	if fail > 0 {
		t.Errorf("Had %d failed requests", fail)
	}
}

// TestRosterRPCSingleEmployeeSnapshot pins bughunt 2026-11-19 L12 through
// the REAL registerRosterRPC wiring: one agents.roster call reads the
// employee list ONCE (the bot exclusion set and the employee rows derive
// from the same snapshot), and the bot/employee split stays exclusive.
// Uses real bot.Store + employee.Manager over throwaway SQLite files.
func TestRosterRPCSingleEmployeeSnapshot(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	botStore, err := bot.NewStore(filepath.Join(dir, "bots.db"))
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { _ = botStore.Close() })
	botMgr := bot.NewManager(botStore, nil)

	cs, err := employee.NewConstitutionStore(filepath.Join(dir, "constitutions.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("constitution store: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	empMgr := employee.NewManagerWithStores(botMgr, botStore, cs, nil, nil, slog.New(slog.DiscardHandler))

	// One plain bot + one hired employee (bot row WITH a constitution).
	if err := botMgr.CreateBot(ctx, bot.BotDefinition{
		ID: "plain-bot", Name: "plain bot", Prompt: "p", Enabled: true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("create plain bot: %v", err)
	}
	if err := botMgr.CreateBot(ctx, bot.BotDefinition{
		ID: "hired-emp", Name: "hired employee", Prompt: "p", Enabled: true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("create employee bot row: %v", err)
	}
	if err := cs.Put(ctx, "hired-emp", employee.Constitution{Purpose: "roster snapshot probe"}); err != nil {
		t.Fatalf("put constitution: %v", err)
	}
	// Re-prime the manager's constitution cache so ListEmployees sees the
	// hire (the store was written after construction).
	empMgr.SetConstitutionStore(cs)

	c := &Components{BotManager: botMgr, EmployeeManager: empMgr}
	srv := rpc.New(&rpc.Config{SocketPath: ""}, nil, slog.New(slog.DiscardHandler))
	registerRosterRPC(c, srv, slog.New(slog.DiscardHandler))

	res, err := srv.CallMethod(ctx, "agents.roster", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("agents.roster: %v", err)
	}
	env, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("roster envelope type %T", res)
	}
	rows, ok := env["agents"].([]rpc.RosterEntry)
	if !ok {
		t.Fatalf("agents rows type %T", env["agents"])
	}
	byID := map[string]rpc.RosterEntry{}
	for _, row := range rows {
		if prev, dup := byID[row.ID]; dup {
			t.Fatalf("duplicate roster row for %q (types %q and %q) — snapshot split broken", row.ID, prev.Type, row.Type)
		}
		byID[row.ID] = row
	}
	botRow, ok := byID["plain-bot"]
	if !ok || botRow.Type != rpc.RosterTypeBot {
		t.Fatalf("plain-bot missing or mistyped: %+v (found=%v)", botRow, ok)
	}
	empRow, ok := byID["hired-emp"]
	if !ok || empRow.Type != rpc.RosterTypeEmployee {
		t.Fatalf("hired-emp missing or mistyped: %+v (found=%v)", empRow, ok)
	}

	// Nil-component degrade: the handler still answers a valid envelope.
	srv2 := rpc.New(&rpc.Config{SocketPath: ""}, nil, slog.New(slog.DiscardHandler))
	registerRosterRPC(nil, srv2, nil)
	res2, err := srv2.CallMethod(ctx, "agents.roster", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("nil-components roster: %v", err)
	}
	env2, ok := res2.(map[string]any)
	if !ok {
		t.Fatalf("nil-components envelope type %T", res2)
	}
	if rows2, ok := env2["agents"].([]rpc.RosterEntry); !ok || len(rows2) != 0 {
		t.Fatalf("nil-components roster = %+v, want an empty non-nil row list", env2["agents"])
	}
}

// TestRosterRPCAdditiveM11Keys pins the M11 population (bughunt
// 2026-11-19, owner-approved) through the REAL registerRosterRPC wiring:
// executor_agent + last_invocation on bot AND employee rows, findings_count
// on employee rows from the audit store's detail-view window, and all four
// keys wire-omitted (byte-identical nine-key row) when no data exists.
// Drift stays zero here because no periodic audit has run for these rows;
// the writer itself is pinned in internal/employee/periodic_auditor_test.go
// TestPeriodicAuditJobWritesDriftScore (the accessor in manager_test.go
// TestManagerDriftScoreAccessor). The findings_count assertions also pin
// the wave-4 exact COUNT: beyond the audit store's 100-row List cap the
// roster count stays exact.
func TestRosterRPCAdditiveM11Keys(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "bots.db")
	logger := slog.New(slog.DiscardHandler)

	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { _ = botStore.Close() })
	botMgr := bot.NewManager(botStore, nil)

	cs, err := employee.NewConstitutionStore(dbPath, logger)
	if err != nil {
		t.Fatalf("constitution store: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	as, err := employee.NewAuditStore(dbPath)
	if err != nil {
		t.Fatalf("audit store: %v", err)
	}
	t.Cleanup(func() { _ = as.Close() })
	empMgr := employee.NewManagerWithStores(botMgr, botStore, cs, nil, as, logger)

	lastRun := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	if err := botMgr.CreateBot(ctx, bot.BotDefinition{
		ID: "exec-bot", Name: "exec bot", Prompt: "p", Enabled: true,
		ExecutorAgent: "hermes",
		Triggers:      []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("create exec bot: %v", err)
	}
	if err := botMgr.CreateBot(ctx, bot.BotDefinition{
		ID: "bare-bot", Name: "bare bot", Prompt: "p", Enabled: true,
		Triggers: []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("create bare bot: %v", err)
	}
	if err := botMgr.CreateBot(ctx, bot.BotDefinition{
		ID: "hired-emp", Name: "hired employee", Prompt: "p", Enabled: true,
		ExecutorAgent: "hermes",
		Triggers:      []bot.BotTrigger{{Type: bot.TriggerTypeWebhook, Enabled: true}},
	}); err != nil {
		t.Fatalf("create employee bot row: %v", err)
	}
	if err := cs.Put(ctx, "hired-emp", employee.Constitution{Purpose: "M11 roster probe"}); err != nil {
		t.Fatalf("put constitution: %v", err)
	}
	empMgr.SetConstitutionStore(cs)

	// Persist a last-run + cost overlay on the exec bot's state row.
	state, err := botStore.GetState(ctx, "exec-bot")
	if err != nil {
		t.Fatalf("get bot state: %v", err)
	}
	state.LastRunAt = &lastRun
	state.TodayCostCents = 77
	if err := botStore.UpdateState(ctx, *state); err != nil {
		t.Fatalf("update bot state: %v", err)
	}

	// Findings: two inside the 168h detail-view window, one outside.
	now := time.Now().UTC()
	for i, f := range []employee.AuditFinding{
		{EmployeeID: "hired-emp", Severity: employee.SeverityWarning, Checkpoint: employee.CheckpointPostTurn, DetectedAt: now.Add(-time.Hour)},
		{EmployeeID: "hired-emp", Severity: employee.SeverityInfo, Checkpoint: employee.CheckpointPostTurn, DetectedAt: now.Add(-2 * time.Hour)},
		{EmployeeID: "hired-emp", Severity: employee.SeverityInfo, Checkpoint: employee.CheckpointPeriodic, DetectedAt: now.Add(-200 * time.Hour)},
	} {
		if err := as.Create(ctx, f); err != nil {
			t.Fatalf("create finding %d: %v", i, err)
		}
	}
	// Wave-4 exact-count pin: 105 more in-window findings push the total
	// (107) past the audit store's default 100-row List cap. The old
	// bucketed-List snapshot reported 100 here; the COUNT path must
	// report the exact 107.
	const extraFindings = 105
	for i := 0; i < extraFindings; i++ {
		if err := as.Create(ctx, employee.AuditFinding{
			EmployeeID: "hired-emp", Severity: employee.SeverityInfo,
			Checkpoint: employee.CheckpointPostTurn,
			DetectedAt: now.Add(-3 * time.Hour),
		}); err != nil {
			t.Fatalf("create extra finding %d: %v", i, err)
		}
	}
	// Sanity: a windowed List really does truncate for this fixture, so
	// the exact-count assertion below exercises the cap, not a no-op.
	windowedList, err := empMgr.ListAuditFindings(ctx, employee.AuditQuery{
		EmployeeID: "hired-emp", Since: 168 * time.Hour,
	})
	if err != nil {
		t.Fatalf("windowed list: %v", err)
	}
	if len(windowedList) != 100 {
		t.Fatalf("windowed List returned %d rows, want the 100-row default cap", len(windowedList))
	}

	c := &Components{BotManager: botMgr, EmployeeManager: empMgr}
	srv := rpc.New(&rpc.Config{SocketPath: ""}, nil, logger)
	registerRosterRPC(c, srv, logger)

	res, err := srv.CallMethod(ctx, "agents.roster", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("agents.roster: %v", err)
	}
	env, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("roster envelope type %T", res)
	}
	rows, ok := env["agents"].([]rpc.RosterEntry)
	if !ok {
		t.Fatalf("agents rows type %T", env["agents"])
	}
	byID := map[string]rpc.RosterEntry{}
	for _, row := range rows {
		byID[row.ID] = row
	}

	execBot, ok := byID["exec-bot"]
	if !ok || execBot.Type != rpc.RosterTypeBot {
		t.Fatalf("exec-bot missing or mistyped: %+v", execBot)
	}
	if execBot.ExecutorAgent != "hermes" {
		t.Errorf("bot executor_agent = %q, want hermes", execBot.ExecutorAgent)
	}
	if !execBot.LastInvocation.Equal(lastRun) {
		t.Errorf("bot last_invocation = %v, want %v", execBot.LastInvocation, lastRun)
	}
	if execBot.FindingsCount != 0 || execBot.DriftScore != 0 {
		t.Errorf("bot row carries employee-only M11 keys: %+v", execBot)
	}

	empRow, ok := byID["hired-emp"]
	if !ok || empRow.Type != rpc.RosterTypeEmployee {
		t.Fatalf("hired-emp missing or mistyped: %+v", empRow)
	}
	if empRow.ExecutorAgent != "hermes" {
		t.Errorf("employee executor_agent = %q, want hermes", empRow.ExecutorAgent)
	}
	if empRow.FindingsCount != 2+extraFindings {
		t.Errorf("employee findings_count = %d, want %d (exact COUNT beyond the 100-row List cap; 168h window excludes the 200h-old finding)", empRow.FindingsCount, 2+extraFindings)
	}
	if empRow.DriftScore != 0 {
		t.Errorf("employee drift_score = %v, want 0 (no periodic audit has run for this row)", empRow.DriftScore)
	}

	// Wire shape: the data-less bot row marshals to exactly the nine C1
	// keys; the populated rows carry their additive keys.
	for id, wantAdditive := range map[string]bool{"bare-bot": false, "exec-bot": true, "hired-emp": true} {
		row, ok := byID[id]
		if !ok {
			t.Fatalf("row %q missing", id)
		}
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatalf("marshal row %q: %v", id, err)
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(raw, &keys); err != nil {
			t.Fatalf("unmarshal row %q: %v", id, err)
		}
		for _, base := range []string{"id", "name", "type", "status", "active_count", "daily_cost_cents", "role", "description", "enabled"} {
			if _, present := keys[base]; !present {
				t.Errorf("row %q lost C1 key %q: %s", id, base, raw)
			}
		}
		_, hasExec := keys["executor_agent"]
		if hasExec != wantAdditive {
			t.Errorf("row %q executor_agent presence = %v, want %v: %s", id, hasExec, wantAdditive, raw)
		}
		if !wantAdditive {
			if len(keys) != 9 {
				t.Errorf("data-less row %q not byte-identical nine-key shape: %s", id, raw)
			}
			for _, k := range []string{"drift_score", "findings_count", "last_invocation"} {
				if _, present := keys[k]; present {
					t.Errorf("data-less row %q leaked additive key %q: %s", id, k, raw)
				}
			}
		}
	}
}
