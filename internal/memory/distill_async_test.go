package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/queue"
)

// NOTE: this test file hand-writes L0 JSONL fixtures using the local
// l0Record mirror — package memory's tests cannot import internal/memory/l0
// (l0 imports memory for SanitizeForStore; an in-package test import would be
// a cycle). The REAL-recorder roundtrip lives in
// distill_async_compat_test.go (package memory_test, no cycle).

// --- fakes ---------------------------------------------------------------

// fakeDistillLLM scripts DistillComplete responses per prompt substring and
// records every prompt it saw.
type fakeDistillLLM struct {
	mu     sync.Mutex
	script []fakeDistillEntry // first matching entry wins; empty match matches any
	prompt string
	calls  int
}

type fakeDistillEntry struct {
	match    string // prompt substring; "" matches anything
	response string
	err      error
}

func (f *fakeDistillLLM) DistillComplete(_ context.Context, prompt string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.prompt = prompt
	for _, e := range f.script {
		if strings.Contains(prompt, e.match) {
			if e.err != nil {
				return nil, e.err
			}
			return []byte(e.response), nil
		}
	}
	return nil, errors.New("fakeDistillLLM: no scripted response for prompt")
}

// fakeEnqueuer captures Enqueue calls.
type fakeEnqueuer struct {
	mu   sync.Mutex
	jobs []*queue.Job
	err  error
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, job *queue.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.jobs = append(f.jobs, job)
	return nil
}

// newDistillTestManager builds an initialized SQLite-backed manager.
func newDistillTestManager(t *testing.T) *Manager {
	t.Helper()
	mgr := NewManager(ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  t.TempDir(),
			Episodic: config.EpisodicConfig{Enabled: true},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

// writeL0Fixture writes n turns for the session into <dataDir>/l0/<today>.jsonl
// using the frozen §2.1 schema (the l0Record mirror) and returns
// (dataDir, l0Path, records written).
func writeL0Fixture(t *testing.T, dataDir, sessionID string, n int) (string, string, []l0Record) {
	t.Helper()
	return writeL0FixtureOn(t, dataDir, sessionID, n, LocalDayKey(time.Now()))
}

// writeL0FixtureOn is writeL0Fixture with an explicit local day key (the H4
// evening pin fixes the day rather than deriving it from the wall clock).
func writeL0FixtureOn(t *testing.T, dataDir, sessionID string, n int, date string) (string, string, []l0Record) {
	t.Helper()
	dir := filepath.Join(dataDir, "l0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir l0: %v", err)
	}
	var recs []l0Record
	var b strings.Builder
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		rec := l0Record{
			ID:        fmt.Sprintf("msg-fixture-%s-%04d", sessionID, i),
			TS:        time.Now().Format(time.RFC3339),
			SessionID: sessionID,
			Role:      role,
			Content:   fmt.Sprintf("turn %d about project alpha", i),
		}
		line, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("marshal fixture: %v", err)
		}
		b.Write(line)
		b.WriteByte('\n')
		recs = append(recs, rec)
	}
	path := filepath.Join(dir, date+".jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dataDir, path, recs
}

func candidateJSON(names ...string) string {
	var b strings.Builder
	b.WriteString("[")
	for i, n := range names {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"type":"claim","text":%q,"source":"conversation","confidence":0.7,"premises":[],"category":"technical"}`, n)
	}
	b.WriteString("]")
	return b.String()
}

func segmentationJSON(segments ...[2]any) string {
	var b strings.Builder
	b.WriteString("[")
	for i, s := range segments {
		if i > 0 {
			b.WriteString(",")
		}
		name := s[0].(string)
		ids := s[1].([]string)
		idJSON, _ := json.Marshal(ids)
		fmt.Fprintf(&b, `{"scene_name":%q,"message_ids":%s}`, name, string(idJSON))
	}
	b.WriteString("]")
	return b.String()
}

func readMarker(t *testing.T, w *DistillWorker, sessionID string) string {
	t.Helper()
	m, err := w.l0ProgressMarker(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	return m
}

func l2Files(t *testing.T, dataDir string) []sceneFile {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dataDir, "l2"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read l2 dir: %v", err)
	}
	var out []sceneFile
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dataDir, "l2", e.Name()))
		if err != nil {
			t.Fatalf("read scene file: %v", err)
		}
		var sf sceneFile
		if err := json.Unmarshal(b, &sf); err != nil {
			t.Fatalf("parse scene file %s: %v", e.Name(), err)
		}
		out = append(out, sf)
	}
	return out
}

// --- L1 tests -------------------------------------------------------------

func TestL1ExtractHappyPathStoresClaimsAndAdvancesMarker(t *testing.T) {
	mgr := newDistillTestManager(t)
	session := "sess-l1-happy"
	dataDir, l0Path, recs := writeL0Fixture(t, t.TempDir(), session, 4)

	llm := &fakeDistillLLM{script: []fakeDistillEntry{
		{match: "", response: candidateJSON("user believes alpha ships friday", "user prefers go")},
	}}
	w := NewDistillWorker(DistillWorkerConfig{
		Manager: mgr,
		LLM:     llm,
		DataDir: dataDir,
		Logger:  slog.New(slog.DiscardHandler),
	})

	payload, err := json.Marshal(L1ExtractPayload{SessionID: session, L0Path: l0Path})
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Process(context.Background(), &queue.Job{Type: MemoryJobL1Extract, Payload: payload})
	if err != nil {
		t.Fatalf("process l1: %v", err)
	}
	got := res.(map[string]any)
	if got["claims_stored"].(int) != 2 {
		t.Fatalf("claims_stored = %v, want 2", got["claims_stored"])
	}

	// Claims stored with status=auto.
	results, err := mgr.Search(context.Background(), MemoryQuery{Type: MemoryTypeClaim, Query: "alpha", Limit: 10})
	if err != nil {
		t.Fatalf("search claims: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no claims found after l1 extract")
	}
	for _, r := range results {
		if status := asString(r.Memory.Metadata["status"]); status != string(ClaimStatusAuto) {
			t.Fatalf("claim status = %q, want auto", status)
		}
	}

	// Marker advanced to the LAST record's ID.
	if m := readMarker(t, w, session); m != recs[len(recs)-1].ID {
		t.Fatalf("marker = %q, want last record id %q", m, recs[len(recs)-1].ID)
	}

	// The extraction prompt carried the conversation.
	if llm.calls != 1 || !strings.Contains(llm.prompt, "turn 3 about project alpha") {
		t.Fatalf("extraction prompt missing conversation; calls=%d", llm.calls)
	}
}

func TestL1ExtractLLMErrorReturnsErrorAndMarkerNotAdvanced(t *testing.T) {
	mgr := newDistillTestManager(t)
	session := "sess-l1-err"
	dataDir, l0Path, _ := writeL0Fixture(t, t.TempDir(), session, 3)

	llm := &fakeDistillLLM{script: []fakeDistillEntry{
		{match: "", err: errors.New("provider down")},
	}}
	w := NewDistillWorker(DistillWorkerConfig{
		Manager: mgr,
		LLM:     llm,
		DataDir: dataDir,
		Logger:  slog.New(slog.DiscardHandler),
	})

	payload, _ := json.Marshal(L1ExtractPayload{SessionID: session, L0Path: l0Path})
	if _, err := w.Process(context.Background(), &queue.Job{Type: MemoryJobL1Extract, Payload: payload}); err == nil {
		t.Fatal("expected error from l1 process, got nil")
	}
	if m := readMarker(t, w, session); m != "" {
		t.Fatalf("marker advanced on llm error: %q", m)
	}
}

func TestL1ExtractDuplicateEnqueueSuppressedByMarker(t *testing.T) {
	mgr := newDistillTestManager(t)
	session := "sess-l1-dup"
	dataDir, l0Path, recs := writeL0Fixture(t, t.TempDir(), session, 2)

	sinceID := recs[len(recs)-1].ID
	// Pre-advance the marker: this window was already distilled.
	w := NewDistillWorker(DistillWorkerConfig{
		Manager: mgr,
		LLM:     &fakeDistillLLM{}, // no script: any real LLM call would fail
		DataDir: dataDir,
		Logger:  slog.New(slog.DiscardHandler),
	})
	if err := w.advanceMarker(context.Background(), session, sinceID); err != nil {
		t.Fatalf("pre-advance marker: %v", err)
	}

	chain := &fakeEnqueuer{}
	w.enqueuer = chain

	payload, _ := json.Marshal(L1ExtractPayload{SessionID: session, L0Path: l0Path, SinceMsgID: sinceID})
	res, err := w.Process(context.Background(), &queue.Job{Type: MemoryJobL1Extract, Payload: payload})
	if err != nil {
		t.Fatalf("duplicate l1 should suppress, not error: %v", err)
	}
	if res.(map[string]any)["status"] != "suppressed" {
		t.Fatalf("status = %v, want suppressed", res.(map[string]any)["status"])
	}
	// No claims were stored.
	results, err := mgr.Search(context.Background(), MemoryQuery{Type: MemoryTypeClaim, Query: "alpha", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("duplicate l1 stored %d claims, want 0", len(results))
	}
	// L2 still chained once.
	if len(chain.jobs) != 1 || chain.jobs[0].Type != MemoryJobL2Scene {
		t.Fatalf("expected exactly one chained l2 job, got %+v", chain.jobs)
	}
}

func TestL1ExtractReadsOnlySincePayloadPosition(t *testing.T) {
	mgr := newDistillTestManager(t)
	session := "sess-l1-since"
	dataDir, _, recs := writeL0Fixture(t, t.TempDir(), session, 4)

	llm := &fakeDistillLLM{script: []fakeDistillEntry{
		{match: "", response: candidateJSON("later belief")},
	}}
	w := NewDistillWorker(DistillWorkerConfig{
		Manager: mgr,
		LLM:     llm,
		DataDir: dataDir,
		Logger:  slog.New(slog.DiscardHandler),
	})

	// Only the last two records are in the window.
	sinceID := recs[1].ID
	l0Path := w.l0DayPath(time.Now().Format("2006-01-02"))
	payload, _ := json.Marshal(L1ExtractPayload{SessionID: session, L0Path: l0Path, SinceMsgID: sinceID})
	if _, err := w.Process(context.Background(), &queue.Job{Type: MemoryJobL1Extract, Payload: payload}); err != nil {
		t.Fatalf("process l1: %v", err)
	}
	prompt := llm.prompt
	if strings.Contains(prompt, "turn 0") || strings.Contains(prompt, "turn 1") {
		t.Fatal("extraction prompt included records before since_msg_id")
	}
	if !strings.Contains(prompt, "turn 2") || !strings.Contains(prompt, "turn 3") {
		t.Fatal("extraction prompt missing window records")
	}
	if m := readMarker(t, w, session); m != recs[3].ID {
		t.Fatalf("marker = %q, want %q", m, recs[3].ID)
	}
}

// --- L2 tests -------------------------------------------------------------

func TestL2SceneHappyPathWritesSceneFiles(t *testing.T) {
	mgr := newDistillTestManager(t)
	session := "sess-l2-happy"
	dataDir, _, recs := writeL0Fixture(t, t.TempDir(), session, 4)

	seg := segmentationJSON(
		[2]any{"alpha planning", []string{recs[0].ID, recs[1].ID}},
		[2]any{"wrap up", []string{recs[2].ID, recs[3].ID}},
	)
	llm := &fakeDistillLLM{script: []fakeDistillEntry{{match: "", response: seg}}}
	w := NewDistillWorker(DistillWorkerConfig{
		Manager: mgr,
		LLM:     llm,
		DataDir: dataDir,
		Logger:  slog.New(slog.DiscardHandler),
	})

	payload, _ := json.Marshal(L2ScenePayload{SessionID: session, SinceTS: time.Now().Format(time.RFC3339)})
	res, err := w.Process(context.Background(), &queue.Job{Type: MemoryJobL2Scene, Payload: payload})
	if err != nil {
		t.Fatalf("process l2: %v", err)
	}
	if res.(map[string]any)["scenes"].(int) != 2 {
		t.Fatalf("scenes = %v, want 2", res.(map[string]any)["scenes"])
	}

	files := l2Files(t, dataDir)
	if len(files) != 2 {
		t.Fatalf("wrote %d scene files, want 2", len(files))
	}
	names := map[string]sceneFile{}
	for _, f := range files {
		names[f.SceneName] = f
		if f.SegmentMethod != "llm" {
			t.Fatalf("scene %q segment_method = %q, want llm", f.SceneName, f.SegmentMethod)
		}
		if f.SessionID != session || f.Date != time.Now().Format("2006-01-02") {
			t.Fatalf("scene %q session/date = %q/%q", f.SceneName, f.SessionID, f.Date)
		}
		if f.Summary == "" {
			t.Fatalf("scene %q has empty summary", f.SceneName)
		}
	}
	if len(names["alpha planning"].SourceMessageIDs) != 2 {
		t.Fatal("alpha planning segment lost message ids")
	}
	// Filenames sanitized: no spaces or separators.
	entries, _ := os.ReadDir(filepath.Join(dataDir, "l2"))
	for _, e := range entries {
		if strings.ContainsAny(e.Name(), " /\\") {
			t.Fatalf("unsanitized scene filename: %s", e.Name())
		}
	}
}

func TestL2SceneMalformedSegmentationFallsBackToWholeSession(t *testing.T) {
	mgr := newDistillTestManager(t)
	session := "sess-l2-bad"
	dataDir, _, recs := writeL0Fixture(t, t.TempDir(), session, 3)

	llm := &fakeDistillLLM{script: []fakeDistillEntry{
		{match: "", response: "I cannot produce JSON for this, sorry: [[["},
	}}
	w := NewDistillWorker(DistillWorkerConfig{
		Manager: mgr,
		LLM:     llm,
		DataDir: dataDir,
		Logger:  slog.New(slog.DiscardHandler),
	})

	payload, _ := json.Marshal(L2ScenePayload{SessionID: session, SinceTS: time.Now().Format(time.RFC3339)})
	res, err := w.Process(context.Background(), &queue.Job{Type: MemoryJobL2Scene, Payload: payload})
	if err != nil {
		t.Fatalf("l2 must not dead-letter on malformed segmentation: %v", err)
	}
	if res.(map[string]any)["method"] != "fallback" {
		t.Fatalf("method = %v, want fallback", res.(map[string]any)["method"])
	}

	files := l2Files(t, dataDir)
	if len(files) != 1 {
		t.Fatalf("fallback wrote %d files, want 1", len(files))
	}
	f := files[0]
	if f.SegmentMethod != "fallback" {
		t.Fatalf("segment_method = %q, want fallback", f.SegmentMethod)
	}
	if len(f.SourceMessageIDs) != len(recs) {
		t.Fatalf("fallback source ids = %d, want all %d", len(f.SourceMessageIDs), len(recs))
	}
}

func TestL2SceneUnknownIDsOnlySegmentDroppedAndRestKept(t *testing.T) {
	mgr := newDistillTestManager(t)
	session := "sess-l2-partial"
	dataDir, _, recs := writeL0Fixture(t, t.TempDir(), session, 3)

	// Segment 1 references only unknown ids (dropped); segment 2 is valid.
	seg := segmentationJSON(
		[2]any{"ghost scene", []string{"msg-nonexistent-1", "msg-nonexistent-2"}},
		[2]any{"real scene", []string{recs[0].ID, recs[1].ID, recs[2].ID}},
	)
	llm := &fakeDistillLLM{script: []fakeDistillEntry{{match: "", response: seg}}}
	w := NewDistillWorker(DistillWorkerConfig{
		Manager: mgr,
		LLM:     llm,
		DataDir: dataDir,
		Logger:  slog.New(slog.DiscardHandler),
	})

	payload, _ := json.Marshal(L2ScenePayload{SessionID: session, SinceTS: time.Now().Format(time.RFC3339)})
	if _, err := w.Process(context.Background(), &queue.Job{Type: MemoryJobL2Scene, Payload: payload}); err != nil {
		t.Fatalf("process l2: %v", err)
	}
	files := l2Files(t, dataDir)
	if len(files) != 1 || files[0].SceneName != "real scene" {
		t.Fatalf("want exactly the valid scene, got %+v", files)
	}
}

// TestChainL2EveningLocalDayReadsSameDayFile is the H4 pin: an L1 success at
// 18:00 local in a zone west of UTC chains an L2 job that must resolve the
// SAME local day file the recorder wrote. Pre-fix, chainL2 stamped UTC, so
// the payload resolved to the NEXT day for every local time in the last
// `offset` hours of the day, the pass read a day file that did not exist
// yet, and it reported scenes:0 with no error and no log.
func TestChainL2EveningLocalDayReadsSameDayFile(t *testing.T) {
	mgr := newDistillTestManager(t)
	session := "sess-l2-evening"

	// Negative offset: 18:00 local on 2026-03-10 is already 2026-03-11 UTC —
	// exactly the window that used to break. Fixed zone, not time.Local, so
	// the pin holds under TZ=UTC too.
	loc := time.FixedZone("test-utc-06", -6*60*60)
	evening := time.Date(2026, 3, 10, 18, 0, 0, 0, loc)
	localDay := LocalDayKey(evening)
	if utcDay := evening.UTC().Format("2006-01-02"); utcDay == localDay {
		t.Fatalf("fixture is not in the failing window: local %s == utc %s", localDay, utcDay)
	}

	dataDir, _, recs := writeL0FixtureOn(t, t.TempDir(), session, 3, localDay)

	seg := segmentationJSON([2]any{"evening scene", recordIDs(recs)})
	llm := &fakeDistillLLM{script: []fakeDistillEntry{{match: "", response: seg}}}
	chain := &fakeEnqueuer{}
	w := NewDistillWorker(DistillWorkerConfig{
		Manager:  mgr,
		LLM:      llm,
		DataDir:  dataDir,
		Enqueuer: chain,
		Now:      func() time.Time { return evening },
		Logger:   slog.New(slog.DiscardHandler),
	})

	w.chainL2(context.Background(), session)
	if len(chain.jobs) != 1 {
		t.Fatalf("chained jobs = %d, want 1", len(chain.jobs))
	}
	var p L2ScenePayload
	if err := json.Unmarshal(chain.jobs[0].Payload, &p); err != nil {
		t.Fatalf("chained l2 payload: %v", err)
	}
	ts, err := time.Parse(time.RFC3339, p.SinceTS)
	if err != nil {
		t.Fatalf("parse chained since_ts %q: %v", p.SinceTS, err)
	}
	if got := LocalDayKey(ts); got != localDay {
		t.Fatalf("chained l2 day = %s, want %s (since_ts %q)", got, localDay, p.SinceTS)
	}

	// The chained job must FIND the day's records: pre-fix this returned
	// {"status":"ok","scenes":0} and wrote no scene file.
	res, err := w.Process(context.Background(), chain.jobs[0])
	if err != nil {
		t.Fatalf("l2 process: %v", err)
	}
	if n := res.(map[string]any)["scenes"].(int); n != 1 {
		t.Fatalf("l2 chained at 18:00 local wrote %d scenes, want 1 (res %v)", n, res)
	}
}

// --- wiring-shape tests ---------------------------------------------------

func TestEnqueueL1AndL2MarshalFrozenPayloads(t *testing.T) {
	ctx := context.Background()
	fq := &fakeEnqueuer{}

	l1ID, err := EnqueueL1(ctx, fq, L1ExtractPayload{SessionID: "s", L0Path: "/p", SinceMsgID: "m1"})
	if err != nil {
		t.Fatalf("enqueue l1: %v", err)
	}
	if l1ID == "" || len(fq.jobs) != 1 {
		t.Fatalf("l1 enqueue id=%q jobs=%d", l1ID, len(fq.jobs))
	}
	var p1 L1ExtractPayload
	if err := json.Unmarshal(fq.jobs[0].Payload, &p1); err != nil {
		t.Fatalf("l1 payload unmarshal: %v", err)
	}
	if p1.SessionID != "s" || p1.L0Path != "/p" || p1.SinceMsgID != "m1" {
		t.Fatalf("l1 payload = %+v", p1)
	}

	_, err = EnqueueL2(ctx, fq, L2ScenePayload{SessionID: "s", SinceTS: "2026-10-13T10:00:00Z"})
	if err != nil {
		t.Fatalf("enqueue l2: %v", err)
	}
	var p2 L2ScenePayload
	if err := json.Unmarshal(fq.jobs[1].Payload, &p2); err != nil {
		t.Fatalf("l2 payload unmarshal: %v", err)
	}
	if p2.SessionID != "s" {
		t.Fatalf("l2 payload = %+v", p2)
	}
	if fq.jobs[0].Type != MemoryJobL1Extract || fq.jobs[1].Type != MemoryJobL2Scene {
		t.Fatalf("job types = %q / %q", fq.jobs[0].Type, fq.jobs[1].Type)
	}
}

func TestDistillWorkerRejectsUnknownJobType(t *testing.T) {
	w := NewDistillWorker(DistillWorkerConfig{Logger: slog.New(slog.DiscardHandler)})
	if _, err := w.Process(context.Background(), &queue.Job{Type: queue.JobType("memory.other"), Payload: []byte("{}")}); err == nil {
		t.Fatal("expected error for unknown job type")
	}
}

func TestSceneSlugSanitizesFilenames(t *testing.T) {
	cases := map[string]string{
		"Alpha Planning!": "alpha-planning",
		"  wrap  up  ":    "wrap-up",
		"café résumé":     "caf-r-sum",
		"":                "scene",
		"!!!":             "scene",
		"very long scene name that goes on and on and should be capped somewhere": "very-long-scene-name-that-goes-on-and-on-and-sho",
	}
	for in, want := range cases {
		if got := sceneSlug(in); got != want {
			t.Errorf("sceneSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
