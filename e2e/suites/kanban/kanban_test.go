//go:build e2e

// Package kanban is the hermetic e2e suite for the LIVE daemon kanban
// wiring (plan tree kanban-board leaf 06/02; master.md Integration Test
// Plan cases (a)-(g)). Every assertion drives a real meept-daemon booted
// by the harness with [kanban].enabled — no hand-replicated fixtures: the
// registration itself (case g) is what the other cases depend on, so the
// daemon's own seeding (default board "meept board" + todo/doing/done +
// the projection ruleset) is the fixture.
//
// Coverage (scenario ids in e2e/manifest.json):
//
//	kanban-01  (a) rebuild equality + fail-safe: a REAL claimed job's
//	           StateEvent projects a machine card (live lane); a ghost
//	           task event published over bus.publish with NO source row
//	           still projects (fetch-miss fail-safe, C1 — provenance
//	           degrades to the kind:id fallback title, the projector
//	           never dies); kanban.reconcile.now deletes ONLY the ghost
//	           orphan (the live job's card stays, the NULL-ref manual
//	           card is immune — C3) so board == source, and the second
//	           pass changes zero rows (idempotence: "kill projector,
//	           restart, board == source" — a dead projector is exactly a
//	           lost-event board and reconcile is its committed recovery
//	           lane).
//	kanban-02  (b) lease refusal against the REAL C10 LeaseAuthority
//	           (internal/daemon/kanban_wiring.go): a job claimed over the
//	           live queue RPC surface holds a claim, so the human
//	           kanban.card.move of its projected card into the machine's
//	           lane refuses with the frozen C1 text "lease held by
//	           <holder> since <RFC3339>" (holder + age) over the error
//	           path — and the refusal moves nothing and writes no
//	           timeline rows.
//	kanban-03  (c) takeover moves + audits: kanban.card.takeover appends
//	           the kind=takeover timeline row FIRST and releases the
//	           lease (queue.db job back to pending via the daemon
//	           authority's Requeue); the machine lane then settles the
//	           card into its claimed->doing projection target, and the
//	           human move that was just refused then LANDS into done with
//	           the timeline ordering takeover-row strictly before
//	           move-row. Timeline created_at is SECOND precision
//	           (RFC3339 in store.go) with a random-id tiebreak, so the
//	           two writes are deliberately separated across a wall-clock
//	           second boundary to make the ordering assertion
//	           deterministic rather than coin-flipped.
//	kanban-04  (d) wip_mode=block: a move onto a column already at its
//	           WIP limit refuses with the exact frozen C1 text "WIP limit
//	           reached for column <id>: 1", the card stays put, and the
//	           refusal writes NO timeline rows.
//	kanban-05  (d) wip_mode=warn (the config default — pinned by NOT
//	           overriding it): the over-WIP move LANDS and the timeline
//	           carries both the move row and a wip_warning note row.
//	kanban-06  (e) pairing lane end-to-end: kanban.pair.mint →
//	           kanban.pair.exchange over the daemon socket persist the
//	           bearer token to $MEEPT_HOME/kanban/tokens.json — the SAME
//	           file the real meept-kanban binary loads as its
//	           GrantChecker — so the exchanged token answers 200 through
//	           the sidecar (forwarding to the live daemon's seeded
//	           board), an absent token 401, and a read-only token 403 on
//	           a mutate route (C5 grant ladder). The code is single-use:
//	           a second exchange fails with the no-oracle text. (REST
//	           401-on-empty-store and the C7 LAN-bind guard are pinned
//	           by the kanban-board suite's kanban-board-01/02 — not
//	           duplicated here.)
//	kanban-07  (f) GUI parity: manifest suite kanban-dart runs
//	           `flutter test test/kanban` via the runner:dart dispatch
//	           (the gui-flows pattern), so a kanban Dart-surface change
//	           gates on the board widget/service/nav tests. Declared as
//	           a manifest row, not a Go test — scripts/e2e-affected.sh
//	           dispatches it by runner.
//	kanban-08  (g) live wiring pins: a DEFAULT boot (no [kanban] block)
//	           answers kanban.board.list with the seeded default board
//	           over the Unix socket (both [kanban].enabled and
//	           [kanban].sidecar_enabled default true, option B) AND seeds
//	           $MEEPT_HOME/dev_key into the shared sidecar token store
//	           with all three board grants before spawn; an EXPLICIT
//	           disabled boot still answers method-not-found (-32601) for
//	           two C4 names while the same socket serves queue.* — the
//	           opt-out posture survives the default flip.
//
// Hermetic: 127.0.0.1 + unix sockets only; no LLM turn is ever driven.
// The lease cases use the daemon's REAL queue machinery over its own RPC
// surface (queue.enqueue/queue.claim) with a capability-isolated job so
// the pool workers can never race our claim (Job.CanBeClaimedBy filters
// by required_caps) — no test-binary store injection anywhere.
//
// Run via `make e2e-fast-area AREA=kanban`.
package kanban

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// pollMachineCard waits for the projector (live lane, async worker) to
// place the machine card for work ref (kind,id) into one of the board's
// columns and returns its card id.
func pollMachineCard(t *testing.T, s *harness.Stack, colIDs map[string]string, kind, refID string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, name := range []string{"todo", "doing", "done"} {
			for _, c := range cardsInColumn(t, s, colIDs[name]) {
				ref, _ := c["ref"].(map[string]any)
				if ref != nil && str(ref, "kind") == kind && str(ref, "id") == refID {
					return str(c, "id")
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("projector never placed a %s card for ref %s", kind, refID)
	return ""
}

// pollCardColumn waits until the card sits in wantCol (the async
// projector's machine-lane move of the card settles).
func pollCardColumn(t *testing.T, s *harness.Stack, cardID, wantCol, why string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if cardColumn(t, s, cardID) == wantCol {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("card %s never settled in column %s (%s); last column %q", cardID, wantCol, why, cardColumn(t, s, cardID))
}

// ---------------------------------------------------------------------------
// kanban-01 — (a) rebuild equality + fail-safe
// ---------------------------------------------------------------------------

func TestRebuildEqualityAndFailSafe(t *testing.T) {
	s := startEnabled(t, nil)
	boardID, cols := defaultBoard(t, s)

	// Live lane: a real job the daemon knows about. Enqueue with an
	// isolated capability (no pool worker holds it) and claim over the
	// queue RPC surface: the claim makes the queue machine publish the
	// pending->claimed StateEvent (gate armed by [kanban].enabled), and
	// the projector mirrors it into a machine card (from=pending maps to
	// the todo column).
	capName := fmt.Sprintf("kanban-e2e-a-%d", time.Now().UnixNano())
	jobID := enqueueIsolatedJob(t, s, capName)
	if got := claimJob(t, s, "e2e-kanban-worker-a", capName); got != jobID {
		t.Fatalf("queue.claim returned job %q, want %q", got, jobID)
	}
	machineCard := pollMachineCard(t, s, cols, "job", jobID)
	if got := cardColumn(t, s, machineCard); got != cols["todo"] {
		t.Fatalf("claimed job's projected card sits in %q, want todo (pending->todo rule)", got)
	}

	// Manual NULL-ref card (C3 immunity case): reconcile must never touch
	// it.
	manualCard := createManualCard(t, s, boardID, cols["done"], "manual keepsake")

	// Fail-safe (C1/C2): a StateEvent for a task that does NOT exist in
	// the task store, delivered over the live bus via the bus.publish
	// RPC. The projector must still project it — the provenance fetch
	// miss degrades to the fallback title, it never drops the event or
	// kills the projector.
	ghostID := fmt.Sprintf("task-e2e-ghost-%d", time.Now().UnixNano())
	deliver := rpcResult(t, s, "bus.publish", map[string]any{
		"topic": "kanban.event.task.state",
		"payload": map[string]any{
			"kind":   "task",
			"id":     ghostID,
			"from":   "pending",
			"to":     "planning",
			"reason": "e2e fail-safe probe",
		},
	})
	if d, _ := deliver["delivered"].(float64); d < 1 {
		t.Fatalf("bus.publish delivered %v, want >= 1 subscriber (projector not on the live lane?): %v", deliver["delivered"], deliver)
	}
	ghostCard := pollMachineCard(t, s, cols, "task", ghostID)
	// The fail-safe projection carries the C1 fallback title "kind:id"
	// (the RowFetcher could not resolve a source row).
	if title := str(rpcResult(t, s, "kanban.card.get", map[string]any{"card_id": ghostCard})["card"].(map[string]any), "title"); !strings.HasPrefix(title, "task:") {
		t.Fatalf("ghost card title %q, want the fetch-miss fallback prefix \"task:\"", title)
	}

	// Authoritative lane: reconcile rebuilds board == source. The ghost
	// is the only orphan (its task is absent from the live enumeration);
	// the claimed job stays enumerated; the NULL-ref manual card is
	// excluded from the scan entirely.
	first := rpcResult(t, s, "kanban.reconcile.now", nil)
	if d, _ := first["deleted"].(float64); d != 1 {
		t.Fatalf("reconcile pass 1 deleted %v, want exactly 1 (the ghost orphan): %v", first["deleted"], first)
	}

	// board == source, component by component.
	if got := cardColumn(t, s, machineCard); got != cols["todo"] {
		t.Fatalf("machine card (claimed job, enumerated from_state pending) sits in %q, want todo %q", got, cols["todo"])
	}
	if got := cardColumn(t, s, manualCard); got != cols["done"] {
		t.Fatalf("NULL-ref manual card moved by reconcile to %q — C3 says manual cards are excluded from the scan", got)
	}
	if msg := rpcErr(t, s, "kanban.card.get", map[string]any{"card_id": ghostCard}); !strings.Contains(msg, "unknown card") {
		t.Fatalf("ghost card survived reconcile: error %q, want the C1 unknown-card text", msg)
	}

	// Idempotence: a second pass over the now-equal board changes zero
	// rows ("after rebuild, board == source" — the rebuild target is
	// fixed, not oscillating).
	second := rpcResult(t, s, "kanban.reconcile.now", nil)
	if tot, _ := second["total"].(float64); tot != 0 {
		t.Fatalf("reconcile is not idempotent — second pass changed %v rows: %v", second["total"], second)
	}
}

// ---------------------------------------------------------------------------
// kanban-02 / kanban-03 — (b)/(c) lease refusal + takeover audit ordering
// against the REAL queue machinery (07/01 daemonLeaseAuthority)
// ---------------------------------------------------------------------------

// claimedJobCard runs the shared (b)/(c) setup: an isolated job claimed
// over the live queue RPC surface, its projector card located.
// Returns (jobID, cardID, columns).
func claimedJobCard(t *testing.T, s *harness.Stack, tag string) (string, string, map[string]string) {
	t.Helper()
	_, cols := defaultBoard(t, s)
	capName := fmt.Sprintf("kanban-e2e-%s-%d", tag, time.Now().UnixNano())
	jobID := enqueueIsolatedJob(t, s, capName)
	worker := "e2e-kanban-worker-" + tag
	if got := claimJob(t, s, worker, capName); got != jobID {
		t.Fatalf("queue.claim returned %q, want %q", got, jobID)
	}
	cardID := pollMachineCard(t, s, cols, "job", jobID)
	return jobID, cardID, cols
}

func TestLeaseRefusalOnClaimedJob(t *testing.T) {
	s := startEnabled(t, nil)
	jobID, cardID, cols := claimedJobCard(t, s, "b")

	// Sanity: the claim landed where the lease authority reads it.
	if state, _ := queueJobState(t, s, jobID); state != "claimed" {
		t.Fatalf("job %s state = %q before the move, want claimed", jobID, state)
	}

	// Lease rule A (C10): a human move of the card INTO its own kind's
	// projected lane (doing — the claimed->doing rule target) while the
	// live claim is held is REFUSED with the frozen C1 text — holder +
	// age included — over the transport error path, never a success
	// envelope (the C1 strings are a three-surface wire contract).
	msg := rpcErr(t, s, "kanban.card.move", map[string]any{
		"card_id": cardID, "column_id": cols["doing"], "actor": "e2e-operator",
	})
	if !strings.Contains(msg, "lease held by e2e-kanban-worker-b since ") {
		t.Fatalf("move refusal = %q, want the frozen C1 text %q", msg, "lease held by <holder> since <RFC3339>")
	}

	// The refusal changed nothing: the card is still in its projected
	// column and the human lane wrote no timeline rows (refusals publish
	// nothing — C2/C3).
	if got := cardColumn(t, s, cardID); got != cols["todo"] {
		t.Fatalf("card moved to %q despite the lease refusal", got)
	}
	if evs := timeline(t, s, cardID); len(evs) != 0 {
		t.Fatalf("lease refusal wrote %d timeline rows, want 0: %v", len(evs), evs)
	}
}

func TestTakeoverAuditOrderingAndLeaseRelease(t *testing.T) {
	s := startEnabled(t, nil)
	jobID, cardID, cols := claimedJobCard(t, s, "c")

	// Precondition: the lease blocks the machine-lane move (case b again,
	// from this boot — takeover must be what unblocks it).
	if msg := rpcErr(t, s, "kanban.card.move", map[string]any{
		"card_id": cardID, "column_id": cols["doing"], "actor": "e2e-operator",
	}); !strings.Contains(msg, "lease held by") {
		t.Fatalf("pre-takeover move should be lease-refused, got %q", msg)
	}

	// Bare takeover (no target column): the C10 bypass — audit row FIRST,
	// then cancel-episode + release-token. It must not move the card
	// itself and must not stamp lease_blocked (that belongs to
	// takeover-then-move).
	tk := rpcResult(t, s, "kanban.card.takeover", map[string]any{"card_id": cardID, "actor": "e2e-operator"})
	if blocked, _ := tk["lease_blocked"].(bool); blocked {
		t.Fatalf("bare takeover answered lease_blocked:true — the stamp belongs to takeover-then-move only")
	}

	// Lease released: the queue row went back to pending through the
	// daemon authority's ReleaseToken (PersistentQueue.Requeue), read
	// straight from queue.db.
	if state, _ := queueJobState(t, s, jobID); state != "pending" {
		t.Fatalf("job %s state after takeover = %q, want pending (lease released)", jobID, state)
	}

	// The takeover's requeue is itself a machine transition: the queue
	// publishes the claimed->pending StateEvent and the projector (which
	// performs NO lease check — machine lane) settles the card into the
	// claimed->doing rule target. Wait for that to settle so no machine
	// event is in flight when the human moves the card, and so the
	// takeover timeline row is safely in an earlier wall-clock second
	// than the move row below (created_at is second-precision with a
	// random-id tiebreak — the boundary crossing makes the ordering
	// assertion deterministic).
	pollCardColumn(t, s, cardID, cols["doing"], "projector mirroring the takeover's claimed->pending requeue")
	waitSecondBoundary(t)

	// The move that was refused while the lease was held now lands two
	// columns over — the human lane is free again (job pending, no live
	// claim).
	rpcResult(t, s, "kanban.card.move", map[string]any{
		"card_id": cardID, "column_id": cols["done"], "actor": "e2e-operator",
	})
	if got := cardColumn(t, s, cardID); got != cols["done"] {
		t.Fatalf("post-takeover move did not land: card in %q, want done %q", got, cols["done"])
	}

	evs := timeline(t, s, cardID)
	if len(evs) != 2 {
		t.Fatalf("timeline has %d rows, want exactly [takeover, move]: %v", len(evs), evs)
	}
	if str(evs[0], "kind") != "takeover" || str(evs[1], "kind") != "move" {
		t.Fatalf("timeline order = [%s, %s], want [takeover, move]", str(evs[0], "kind"), str(evs[1], "kind"))
	}
	ts0, ts1 := str(evs[0], "created_at"), str(evs[1], "created_at")
	if ts0 >= ts1 {
		t.Fatalf("takeover audit row (created_at %s) is not strictly before the move row (%s) — audit-before-effect violated", ts0, ts1)
	}
}

// ---------------------------------------------------------------------------
// kanban-04 / kanban-05 — (d) WIP counter accuracy per wip_mode
// ---------------------------------------------------------------------------

// wipFixture caps the done column at WIP=1 with one card already in it
// and returns a fresh second card sitting in todo.
func wipFixture(t *testing.T, s *harness.Stack) (string, string, map[string]string) {
	t.Helper()
	boardID, cols := defaultBoard(t, s)
	rpcResult(t, s, "kanban.column.set_wip", map[string]any{"column_id": cols["done"], "wip": 1})
	card1 := createManualCard(t, s, boardID, cols["done"], "wip occupant")
	card2 := createManualCard(t, s, boardID, cols["todo"], "wip challenger")
	return card1, card2, cols
}

func TestWIPBlockRefusesOverLimitMove(t *testing.T) {
	s := startEnabled(t, map[string]any{"kanban.wip_mode": "block"})
	_, card2, cols := wipFixture(t, s)

	// Counter accuracy: the target column already holds its 1 allowed
	// card, so the challenger's move refuses with the exact frozen C1
	// text BEFORE the write.
	msg := rpcErr(t, s, "kanban.card.move", map[string]any{
		"card_id": card2, "column_id": cols["done"], "actor": "e2e-operator",
	})
	want := fmt.Sprintf("WIP limit reached for column %s: 1", cols["done"])
	if msg != want {
		t.Fatalf("block-mode refusal = %q, want the exact C1 text %q", msg, want)
	}
	if got := cardColumn(t, s, card2); got != cols["todo"] {
		t.Fatalf("refused move still moved the card to %q", got)
	}
	if evs := timeline(t, s, card2); len(evs) != 0 {
		t.Fatalf("WIP refusal wrote timeline rows: %v", evs)
	}
}

func TestWIPWarnMovesAndNotesOverLimit(t *testing.T) {
	// The config default is warn; startEnabled WITHOUT a wip_mode overlay
	// pins the default posture rather than an explicit repeat of it.
	s := startEnabled(t, nil)
	_, card2, cols := wipFixture(t, s)

	rpcResult(t, s, "kanban.card.move", map[string]any{
		"card_id": card2, "column_id": cols["done"], "actor": "e2e-operator",
	})
	if got := cardColumn(t, s, card2); got != cols["done"] {
		t.Fatalf("warn-mode over-WIP move did not land: card in %q, want done", got)
	}

	// The move row and its wip_warning note row land inside one call at
	// second-precision created_at, so their ORDER within the column is a
	// random id tiebreak — assert the SET of kinds, not the sequence
	// (C9: warn moves and notes; the note text rides the row body).
	kinds := map[string]int{}
	for _, ev := range timeline(t, s, card2) {
		kinds[str(ev, "kind")]++
	}
	if kinds["move"] != 1 || kinds["wip_warning"] != 1 || len(kinds) != 2 {
		t.Fatalf("warn-mode timeline kinds = %v, want exactly one move + one wip_warning", kinds)
	}
}

// ---------------------------------------------------------------------------
// kanban-06 — (e) pairing lane: daemon mint/exchange -> shared token file
// -> real sidecar bearer + grant ladder
// ---------------------------------------------------------------------------

// repoRoot walks up from the test package dir (e2e/suites/kanban) to the
// module root — the same pattern the kanban-board suite uses to build the
// sidecar binary.
func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cwd: %v", err)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(cwd)))
	if _, err := os.Stat(filepath.Join(root, "cmd", "meept-kanban", "main.go")); err != nil {
		t.Fatalf("module root %s: %v", root, err)
	}
	return root
}

func buildSidecar(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "meept-kanban")
	cmd := exec.Command("go", "build", "-o", out, "./cmd/meept-kanban")
	cmd.Dir = repoRoot(t)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build meept-kanban: %v", err)
	}
	return out
}

func TestPairingLaneGrantLadderAgainstSidecar(t *testing.T) {
	s := startEnabled(t, nil)

	// C7 mint over the owner socket: read-only grant-ladder rung. The
	// reply is the grouped display form (XXXX-XXXX).
	minted := rpcResult(t, s, "kanban.pair.mint", map[string]any{"grants": []string{"board.read"}})
	code := str(minted, "code")
	if len(code) != 9 || code[4] != '-' {
		t.Fatalf("minted code %q, want grouped XXXX-XXXX", code)
	}

	// Exchange consumes the single-use code and mints the bearer token,
	// persisted to $MEEPT_HOME/kanban/tokens.json — the path the
	// sidecar's TokenStore loads by default (DefaultTokenStorePath: the
	// daemon and the sidecar share ONE token file, which is what makes
	// the pairing lane end-to-end).
	exchanged := rpcResult(t, s, "kanban.pair.exchange", map[string]any{"code": code})
	token := str(exchanged, "token")
	if len(token) < 32 {
		t.Fatalf("exchanged token looks truncated (len %d)", len(token))
	}
	grants, _ := exchanged["grants"].([]any)
	if len(grants) != 1 || grants[0] != "board.read" {
		t.Fatalf("token grants = %v, want [board.read]", exchanged["grants"])
	}
	if _, err := os.Stat(filepath.Join(s.MeeptHome, "kanban", "tokens.json")); err != nil {
		t.Fatalf("daemon did not persist the shared token store: %v", err)
	}

	// Single-use: a second exchange of the same code answers the uniform
	// no-oracle refusal.
	if msg := rpcErr(t, s, "kanban.pair.exchange", map[string]any{"code": code}); !strings.Contains(msg, "invalid or already-used") {
		t.Fatalf("re-exchange error = %q, want the single-use refusal", msg)
	}

	// Boot the REAL sidecar pinned to the sandbox MEEPT_HOME (so it
	// loads the daemon-written token file) and the daemon's unix socket.
	bin := buildSidecar(t)
	port := harness.FreePort(t)
	cmd := exec.Command(bin,
		"-bind", fmt.Sprintf("127.0.0.1:%d", port),
		"-daemon-socket", s.SocketPath,
	)
	cmd.Env = append(os.Environ(), "HOME="+s.Home, "MEEPT_HOME="+s.MeeptHome)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sidecar: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	client := &http.Client{Timeout: 10 * time.Second}
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := client.Get(base + "/") //nolint:noctx // bounded readiness poll
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sidecar never listened on %s", base)
		}
		time.Sleep(100 * time.Millisecond)
	}

	req := func(method, path, bearer string) (int, map[string]any) {
		t.Helper()
		r, err := http.NewRequest(method, base+path, strings.NewReader(`{}`)) //nolint:noctx // test-side client carries its own timeout
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		r.Header.Set("Content-Type", "application/json")
		if bearer != "" {
			r.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := client.Do(r)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return resp.StatusCode, body
	}

	// No token -> 401 (bearer lane refuses before any forward).
	if st, _ := req(http.MethodGet, "/api/v1/board/list", ""); st != http.StatusUnauthorized {
		t.Fatalf("board/list without token: status %d, want 401", st)
	}
	// Paired read token -> 200, forwarded to the LIVE daemon: the seeded
	// board visible through the sidecar proves the whole chain — daemon
	// registration -> unix socket -> gateway -> bearer lane -> shared
	// token file.
	st, body := req(http.MethodGet, "/api/v1/board/list", token)
	if st != http.StatusOK {
		t.Fatalf("board/list with paired token: status %d body %v, want 200", st, body)
	}
	if raw, _ := json.Marshal(body); !strings.Contains(string(raw), "meept board") {
		t.Fatalf("sidecar list did not forward the live daemon's seeded board: %s", raw)
	}
	// Grant ladder (C5): a read-only token hitting a mutate route gets
	// 403 — the refusal is the gateway's, before any forward.
	if st, _ := req(http.MethodPost, "/api/v1/card/create", token); st != http.StatusForbidden {
		t.Fatalf("card/create with read-only token: status %d, want 403", st)
	}
}

// ---------------------------------------------------------------------------
// kanban-08 — (g) live wiring pins
// ---------------------------------------------------------------------------

func TestEnabledWiringServesSeededBoard(t *testing.T) {
	s := startEnabled(t, nil)

	// The registration fired: the byte-proof that the daemon's own boot
	// (07/01 wireKanbanServices + registerKanbanRPC), not a test fixture,
	// answers the C4 surface — plus the seeded default board and its
	// todo/doing/done columns.
	out := rpcResult(t, s, "kanban.board.list", nil)
	boards, _ := out["boards"].([]any)
	found := false
	for _, b := range boards {
		if bm, _ := b.(map[string]any); bm != nil && str(bm, "name") == "meept board" {
			found = true
		}
	}
	if !found {
		t.Fatalf("enabled boot does not serve the seeded default board: %v", out)
	}
	_, cols := defaultBoard(t, s)
	for _, name := range []string{"todo", "doing", "done"} {
		if cols[name] == "" {
			t.Fatalf("seeded column %q missing", name)
		}
	}
}

// TestKanbanDefaultBoardListed pins option B (kanban defaults on). A default
// boot — no [kanban] block in meept.json5 — must register the C4 surface and
// serve the daemon-seeded board. Against the old default-off code this is RED
// (kanban.board.list answers -32601 method-not-found).
func TestKanbanDefaultBoardListed(t *testing.T) {
	s := harness.Start(t) // no config overlay: exercise the shipped defaults

	out := rpcResult(t, s, "kanban.board.list", nil)
	boards, _ := out["boards"].([]any)
	if len(boards) == 0 {
		t.Fatalf("default boot served no boards: %v", out)
	}
	found := false
	for _, b := range boards {
		if bm, _ := b.(map[string]any); bm != nil && str(bm, "name") == "meept board" {
			found = true
		}
	}
	if !found {
		t.Fatalf("default boot did not serve the seeded default board %q: %v", "meept board", out)
	}

	// Control: the same socket serves non-kanban methods normally.
	rpcResult(t, s, "queue.stats", nil)
}

// TestKanbanDefaultSeedsDevKeyToken pins the dev-key token seed that must
// land with the default-on flip. Before spawning the sidecar, the daemon
// seeds $MEEPT_HOME/dev_key into the shared token store at
// $MEEPT_HOME/kanban/tokens.json with board.read + board.mutate +
// board.takeover, so a GUI holding the dev key can drive the board without a
// separate pairing handshake. The KEY VALUE IS NEVER READ OR LOGGED: only its
// presence as a map key in tokens.json is asserted. Against current code
// (no seed, store file absent) this is RED.
func TestKanbanDefaultSeedsDevKeyToken(t *testing.T) {
	s := harness.Start(t) // default boot

	// The dev-key file must exist on a default boot (the daemon generates
	// it first-boot). Read it only to confirm presence/length; never print
	// or log the contents.
	keyData, err := os.ReadFile(filepath.Join(s.MeeptHome, "dev_key"))
	if err != nil {
		t.Fatalf("default boot did not provision $MEEPT_HOME/dev_key: %v", err)
	}
	devKey := strings.TrimSpace(string(keyData))
	if devKey == "" {
		t.Fatal("default boot provisioned an empty dev_key")
	}

	// The shared token store is written before the sidecar spawns. Poll
	// briefly to absorb the spawn ordering (the seed is a boot-time write,
	// normally already present by the time the RPC socket answers).
	tokensPath := filepath.Join(s.MeeptHome, "kanban", "tokens.json")
	var rec struct {
		Version int                 `json:"version"`
		Tokens  map[string][]string `json:"tokens"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(tokensPath)
		if err == nil {
			if jerr := json.Unmarshal(data, &rec); jerr != nil {
				t.Fatalf("decode %s: %v", tokensPath, jerr)
			}
			if _, ok := rec.Tokens[devKey]; ok {
				break
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("default boot did not seed the sidecar token store at %s: %v", tokensPath, err)
			}
			t.Fatalf("default boot did not seed the dev key into %s (token count %d)", tokensPath, len(rec.Tokens))
		}
		time.Sleep(50 * time.Millisecond)
	}

	// The dev key must carry ALL three C5 board grants (order-independent).
	grants := rec.Tokens[devKey]
	want := map[string]bool{"board.read": true, "board.mutate": true, "board.takeover": true}
	for _, g := range grants {
		delete(want, g)
	}
	if len(want) != 0 {
		t.Fatalf("dev-key token grants missing %v (got %v)", want, grants)
	}
}

// TestKanbanExplicitDisabledIsMethodNotFound pins the opt-out contract
// AFTER the default flip: an explicit [kanban].enabled=false AND
// [kanban].sidecar_enabled=false must still leave every C4 name unregistered
// (-32601 method-not-found), while the socket stays healthy. This is the
// surviving "disabled" posture; only the DEFAULT changed.
func TestKanbanExplicitDisabledIsMethodNotFound(t *testing.T) {
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"kanban.enabled":         false,
		"kanban.sidecar_enabled": false,
	}))

	resp := rpcCall(t, s.SocketPath, "kanban.board.list", nil)
	errObj, ok := resp["error"].(map[string]any)
	if !ok || errObj == nil {
		t.Fatalf("explicitly disabled boot ANSWERED kanban.board.list: %v — registration fired despite opt-out", resp["result"])
	}
	if code, _ := errObj["code"].(float64); code != -32601 {
		t.Fatalf("explicitly disabled boot error code %v, want -32601 method-not-found: %v", errObj["code"], errObj)
	}
	if msg, _ := errObj["message"].(string); !strings.Contains(msg, "method not found: kanban.board.list") {
		t.Fatalf("explicitly disabled boot error message %q, want the method-not-found text", errObj["message"])
	}
	// A second C4-surface name pinned dead while disabled.
	if resp := rpcCall(t, s.SocketPath, "kanban.pair.mint", map[string]any{}); resp["error"] == nil {
		t.Fatalf("explicitly disabled boot answered kanban.pair.mint: %v", resp)
	}

	// Control: the socket serves non-kanban methods normally on this boot.
	rpcResult(t, s, "queue.stats", nil)
}
