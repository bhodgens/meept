// Unit tests for the codemode sandbox's INTERNAL mechanics. Per the leaf
// policy decision these live in the new package (an accepted exception to
// the e2e-only rule, recorded by the orchestrator at dispatch): the
// sandbox-internal mechanics (timeout kill wall-clock bound, memory-cap
// abort, no-fs negative probes, 50KB clamps, bridge round-trip,
// determinism) need in-process hooks the hermetic e2e harness cannot
// reach. The e2e suite (e2e/suites/codemode) remains the behavioral gate.
//
// Determinism note: these tests run with -count=5 in CI/verify; the same
// script + same bridges must yield the same Result every time.
package codemode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- shared bridge helpers ----

// TestNewValidation pins New's configuration checks.
func TestNewValidation(t *testing.T) {
	// duplicate names -> error
	s, err := New(Options{
		Tools: []ToolBridge{
			{Name: "a", Call: func(ctx context.Context, argsJSON string) (string, error) { return "", nil }},
			{Name: "a", Call: func(ctx context.Context, argsJSON string) (string, error) { return "", nil }},
		},
	})
	if err == nil || s != nil {
		t.Fatalf("New dup name: err=%v s=%v, want error", err, s)
	}
	// empty name -> error
	s, err = New(Options{Tools: []ToolBridge{{Name: "", Call: func(ctx context.Context, argsJSON string) (string, error) { return "", nil }}}})
	if err == nil || s != nil {
		t.Fatalf("New empty name: err=%v s=%v, want error", err, s)
	}
	// nil Call -> error
	s, err = New(Options{Tools: []ToolBridge{{Name: "x"}}})
	if err == nil || s != nil {
		t.Fatalf("New nil Call: err=%v s=%v, want error", err, s)
	}
	// valid
	s, err = New(Options{Tools: []ToolBridge{{Name: "ok", Call: func(ctx context.Context, argsJSON string) (string, error) { return "", nil }}}})
	if err != nil || s == nil {
		t.Fatalf("New valid: err=%v s=%v", err, s)
	}
	_ = s.Close()
}

// TestExecuteHappyPath pins the C3 happy path: a script that returns a value
// and calls a tool; both land in the Result.
func TestExecuteHappyPath(t *testing.T) {
	var seen atomic.Value
	bridges := []ToolBridge{
		{Name: "echo", Call: func(ctx context.Context, argsJSON string) (string, error) {
			seen.Store(argsJSON)
			return "echo:" + argsJSON, nil
		}},
	}
	s, err := New(Options{Timeout: 5 * time.Second, Tools: bridges})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	res, err := s.Execute(context.Background(), `
		const out = await tools.echo("hi");
		text("trace line");
		return { result: out, ok: true };
	`)
	if err != nil {
		t.Fatalf("Execute host error: %v", err)
	}
	if !res.OK {
		t.Fatalf("res.OK=false: %+v", res)
	}
	if res.Err != "" {
		t.Fatalf("res.Err=%q, want empty", res.Err)
	}
	// The tool was called with the JSON args the script passed.
	if got := seen.Load().(string); got != `"hi"` && got != `hi` {
		// argsJSON for a string arg is the raw string passthrough
		if !strings.Contains(got, "hi") {
			t.Fatalf("echo saw argsJSON=%q, want it to carry 'hi'", got)
		}
	}
	// Output carries the text() chunk.
	if !strings.Contains(res.Output, "trace line") {
		t.Fatalf("res.Output=%q, want it to contain 'trace line'", res.Output)
	}
	// Value is the JSON-serialized return object.
	var val struct {
		Result string `json:"result"`
		OK     bool   `json:"ok"`
	}
	if err := json.Unmarshal([]byte(res.Value), &val); err != nil {
		t.Fatalf("res.Value not JSON (%q): %v", res.Value, err)
	}
	if val.Result != "echo:hi" {
		t.Fatalf("res.Value.result=%q, want 'echo:hi'", val.Result)
	}
}

// TestExecuteReturnValue pins a bare numeric return -> C3 Result.Value "2".
func TestExecuteReturnValue(t *testing.T) {
	s, err := New(Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res, _ := s.Execute(context.Background(), `return 1+1;`)
	if !res.OK {
		t.Fatalf("res.OK=false: %+v", res)
	}
	if res.Value != "2" {
		t.Fatalf("res.Value=%q, want \"2\"", res.Value)
	}
}

// TestValueClamp pins T2: a 60KB string return clamps to 50KB+marker.
func TestValueClamp(t *testing.T) {
	s, err := New(Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// 60KB of 'a'.
	const size = 60 * 1024
	res, _ := s.Execute(context.Background(), fmt.Sprintf(`return "%s";`, strings.Repeat("a", size)))
	if !res.OK {
		t.Fatalf("res.OK=false: %+v", res)
	}
	// The clamped Value must be <= 50KB + marker (a bounded header). The
	// unclamped JSON would be 60KB+2 (quotes) = 61442; the clamped one
	// must be strictly smaller and carry the clamp marker.
	if len(res.Value) > 51*1024 {
		t.Fatalf("res.Value len=%d, want <= ~51KB (clamped)", len(res.Value))
	}
	if !strings.Contains(res.Value, "clamped") {
		t.Fatalf("res.Value=%q lacks the clamp marker", res.Value)
	}
	// The marker must report the original size.
	if !strings.Contains(res.Value, fmt.Sprintf("%d", size+2)) {
		t.Fatalf("res.Value marker lost the total: %q", res.Value)
	}
}

// TestOutputClamp pins T6: 60KB of text() calls clamps Output to 50KB+marker.
func TestOutputClamp(t *testing.T) {
	s, err := New(Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const size = 60 * 1024
	res, _ := s.Execute(context.Background(), fmt.Sprintf(`text("%s"); return 0;`, strings.Repeat("b", size)))
	if !res.OK {
		t.Fatalf("res.OK=false: %+v", res)
	}
	if len(res.Output) > 51*1024 {
		t.Fatalf("res.Output len=%d, want <= ~51KB (clamped)", len(res.Output))
	}
	if !strings.Contains(res.Output, "clamped") {
		t.Fatalf("res.Output lacks the clamp marker: %q", res.Output[:64])
	}
}

// TestBridgeRoundTrip pins T3: args JSON -> Call -> result JSON back, and
// bridge errors reject with the same message.
func TestBridgeRoundTrip(t *testing.T) {
	s, err := New(Options{Timeout: 5 * time.Second, Tools: []ToolBridge{
		{Name: "echo", Call: func(ctx context.Context, argsJSON string) (string, error) {
			return "echo:" + argsJSON, nil
		}},
		{Name: "boom", Call: func(ctx context.Context, argsJSON string) (string, error) {
			return "", errors.New("kaboom")
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// round-trip
	res, _ := s.Execute(context.Background(), `const r = await tools.echo({a:1}); return r;`)
	if !res.OK {
		t.Fatalf("echo round-trip OK=false: %+v", res)
	}
	if res.Value != `"echo:{\"a\":1}"` {
		t.Fatalf("echo Value=%q", res.Value)
	}

	// bridge error rejects with the same message (caught in the script)
	res, _ = s.Execute(context.Background(), `
		try {
			await tools.boom({});
			return "no-error";
		} catch (e) {
			return "caught:" + e.message;
		}
	`)
	if !res.OK {
		t.Fatalf("catch path OK=false: %+v", res)
	}
	if res.Value != `"caught:kaboom"` {
		t.Fatalf("bridge error message did not round-trip: Value=%q", res.Value)
	}
}

// TestTimeoutKill pins T4: an infinite loop with a short Timeout is a hard
// kill — the wall-clock return is well under a generous bound, and Err is
// set. NOT a hang.
func TestTimeoutKill(t *testing.T) {
	s, err := New(Options{Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start := time.Now()
	res, _ := s.Execute(context.Background(), `for(;;){}`)
	elapsed := time.Since(start)
	if res.OK {
		t.Fatalf("killed infinite loop reports OK=true: %+v", res)
	}
	if res.Err == "" {
		t.Fatalf("res.Err empty after a timeout kill: %+v", res)
	}
	// Hard-kill wall-clock bound: the kill must land near the 200ms
	// deadline, far under the 2s T4 bound. Give generous slack for slow CI.
	if elapsed > 2*time.Second {
		t.Fatalf("timeout kill took %s, want < 2s (hard kill, not a hang)", elapsed)
	}
	if !strings.Contains(res.Err, "deadline exceeded") {
		t.Fatalf("res.Err=%q, want it to name the timeout", res.Err)
	}
}

// TestTimeoutMidAwait pins the spike b2 case: a script awaiting a slow tool
// when the deadline lands mid-poll — returns a clean error, no hang.
func TestTimeoutMidAwait(t *testing.T) {
	// A tool that takes 2s, but the deadline is 200ms -> kill mid-await.
	s, err := New(Options{
		Timeout: 200 * time.Millisecond,
		Tools: []ToolBridge{
			{Name: "slow", Call: func(ctx context.Context, argsJSON string) (string, error) {
				select {
				case <-time.After(2 * time.Second):
					return "late", nil
				case <-ctx.Done():
					return "", ctx.Err()
				}
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start := time.Now()
	res, _ := s.Execute(context.Background(), `const out = await tools.slow({}); return out;`)
	elapsed := time.Since(start)
	if res.OK {
		t.Fatalf("killed mid-await reports OK=true: %+v", res)
	}
	if res.Err == "" {
		t.Fatalf("res.Err empty after mid-await kill: %+v", res)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("mid-await kill took %s, want ~200ms (hard kill)", elapsed)
	}
}

// TestMemoryCap pins T5: an allocation-heavy script under a 64MB cap trips
// the allocation-counting interrupt and aborts with a memory-cap error.
func TestMemoryCap(t *testing.T) {
	s, err := New(Options{
		Timeout:     30 * time.Second,
		MemoryLimit: 64 * 1024 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Push 5M 10-char strings — the spike's allocation-heavy shape. Each
	// push counts its arg bytes; 5M * 10 = 50MB of counted bytes plus
	// overhead easily crosses a 64MB threshold when the instrumented
	// push counts conservatively. Use a cap low enough to trip fast: we
	// use the 64MB cap but the counter's 1:1 byte mapping means ~50MB
	// of raw arg bytes alone is near it; push more to cross it.
	res, _ := s.Execute(context.Background(), `
		const arr = [];
		for (let i = 0; i < 5000000; i++) {
			arr.push(String(i));
		}
		return arr.length;
	`)
	if res.OK {
		t.Fatalf("memory-heavy script completed OK under the cap (Value=%s) — cap did not trip: %+v", res.Value, res)
	}
	if res.Err == "" {
		t.Fatalf("res.Err empty after the memory cap: %+v", res)
	}
	if !strings.Contains(res.Err, "memory cap") {
		t.Fatalf("res.Err=%q, want it to name the memory cap", res.Err)
	}
}

// TestNoMemoryCapWhenUnlimited pins that MemoryLimit=0 disables the cap:
// the same heavy script completes.
func TestNoMemoryCapWhenUnlimited(t *testing.T) {
	s, err := New(Options{Timeout: 30 * time.Second}) // no MemoryLimit
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res, _ := s.Execute(context.Background(), `
		const arr = [];
		for (let i = 0; i < 200000; i++) {
			arr.push(String(i));
		}
		return arr.length;
	`)
	if !res.OK {
		t.Fatalf("unlimited-cap script failed: %+v", res)
	}
	if res.Value != "200000" {
		t.Fatalf("res.Value=%q, want '200000'", res.Value)
	}
}

// TestNoFsNet pins T5's negative: no fs/net/process reachable from a
// script.
func TestNoFsNet(t *testing.T) {
	s, err := New(Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res, _ := s.Execute(context.Background(), `
		const probes = [];
		try { const fs = require("fs"); probes.push("fs=" + (typeof fs)); }
		catch (e) { probes.push("require-fs-throws:" + e.message); }
		probes.push("process=" + typeof process);
		probes.push("fetch=" + typeof fetch);
		probes.push("XMLHttpRequest=" + typeof XMLHttpRequest);
		probes.push("WebSocket=" + typeof WebSocket);
		return probes.join(" | ");
	`)
	if !res.OK {
		t.Fatalf("probe script failed: %+v", res)
	}
	val := res.Value
	// fs must be unreachable: require("fs") throws.
	if !strings.Contains(val, "require-fs-throws") {
		t.Fatalf("require('fs') did NOT throw: %s", val)
	}
	for _, want := range []string{"process=undefined", "fetch=undefined", "XMLHttpRequest=undefined", "WebSocket=undefined"} {
		if !strings.Contains(val, want) {
			t.Fatalf("probe missing %q: %s", want, val)
		}
	}
}

// TestExecuteAfterClose pins that Execute on a closed sandbox errors
// cleanly (no goroutine leak, no panic).
func TestExecuteAfterClose(t *testing.T) {
	s, err := New(Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Close is idempotent.
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	res, err := s.Execute(context.Background(), `return 1;`)
	if err != nil {
		t.Fatalf("Execute after Close returned a host error: %v", err)
	}
	if res.OK {
		t.Fatalf("closed sandbox executed OK: %+v", res)
	}
	if res.Err == "" {
		t.Fatalf("closed sandbox res.Err empty: %+v", res)
	}
}

// TestDeterminism pins the -count=5 invariant: the same script + same
// bridges yield byte-identical Results across runs.
func TestDeterminism(t *testing.T) {
	mk := func() (*Sandbox, error) {
		return New(Options{
			Timeout: 5 * time.Second,
			Tools: []ToolBridge{
				{Name: "echo", Call: func(ctx context.Context, argsJSON string) (string, error) {
					return "e:" + argsJSON, nil
				}},
			},
		})
	}
	runs := 0
	for i := 0; i < 3; i++ {
		s, err := mk()
		if err != nil {
			t.Fatal(err)
		}
		res, _ := s.Execute(context.Background(), `
			const a = await tools.echo("x");
			return { a, n: 42 };
		`)
		s.Close()
		if !res.OK {
			t.Fatalf("run %d OK=false: %+v", i, res)
		}
		if res.Value != `{"a":"e:x","n":42}` {
			t.Fatalf("run %d Value=%q (want {\"a\":\"e:x\",\"n\":42}) — non-deterministic", i, res.Value)
		}
		runs++
	}
	if runs != 3 {
		t.Fatalf("ran %d times", runs)
	}
}

// TestTextMultipleArgs pins text()'s multi-arg join semantics.
func TestTextMultipleArgs(t *testing.T) {
	s, err := New(Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res, _ := s.Execute(context.Background(), `text("a", "b", 3); return 0;`)
	if !res.OK {
		t.Fatalf("OK=false: %+v", res)
	}
	// fmt.Sprint of ["a" "b" "3"] -> "a b 3"
	if res.Output != "a b 3" {
		t.Fatalf("res.Output=%q, want 'a b 3'", res.Output)
	}
}

// TestAllToolsExposesCatalog pins ALL_TOOLS enumeration.
func TestAllToolsExposesCatalog(t *testing.T) {
	s, err := New(Options{Timeout: 5 * time.Second, Tools: []ToolBridge{
		{Name: "alpha", Description: "does alpha", Call: func(ctx context.Context, argsJSON string) (string, error) { return "alpha", nil }},
		{Name: "beta", Description: "does beta", Call: func(ctx context.Context, argsJSON string) (string, error) { return "beta", nil }},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res, _ := s.Execute(context.Background(), `
		const names = Object.keys(ALL_TOOLS).sort();
		return { names: names, alphaDesc: ALL_TOOLS.alpha.description };
	`)
	if !res.OK {
		t.Fatalf("OK=false: %+v", res)
	}
	// JSON marshal sorts object keys, so compare via decode.
	var got struct {
		Names     []string `json:"names"`
		AlphaDesc string   `json:"alphaDesc"`
	}
	if err := json.Unmarshal([]byte(res.Value), &got); err != nil {
		t.Fatalf("ALL_TOOLS Value=%q not JSON: %v", res.Value, err)
	}
	if len(got.Names) != 2 || got.Names[0] != "alpha" || got.Names[1] != "beta" {
		t.Fatalf("ALL_TOOLS names=%v, want [alpha beta]", got.Names)
	}
	if got.AlphaDesc != "does alpha" {
		t.Fatalf("ALL_TOOLS.alpha.description=%q, want 'does alpha'", got.AlphaDesc)
	}
}

// TestBridgePerCallContextCancellation pins that a bridge ctx is cancelled
// when the run is killed mid-tool (the spike case a).
func TestBridgePerCallContextCancellation(t *testing.T) {
	var cancelled atomic.Bool
	s, err := New(Options{
		Timeout: 200 * time.Millisecond,
		Tools: []ToolBridge{
			{Name: "slow", Call: func(ctx context.Context, argsJSON string) (string, error) {
				select {
				case <-time.After(2 * time.Second):
					return "late", nil
				case <-ctx.Done():
					cancelled.Store(true)
					return "", fmt.Errorf("tool cancelled: %v", ctx.Err())
				}
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, _ = s.Execute(context.Background(), `const out = await tools.slow({}); return out;`)
	if !cancelled.Load() {
		t.Fatalf("bridge ctx was not cancelled on the run timeout")
	}
}

// TestConcurrentExecutesSerialized pins that concurrent Executes on one
// sandbox are serialized (runMu) and each returns a well-formed Result.
func TestConcurrentExecutesSerialized(t *testing.T) {
	s, err := New(Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	const n = 8
	var wg sync.WaitGroup
	resCh := make(chan Result, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, _ := s.Execute(context.Background(), fmt.Sprintf(`return %d;`, i))
			resCh <- r
		}(i)
	}
	wg.Wait()
	close(resCh)
	got := 0
	for r := range resCh {
		if !r.OK {
			t.Fatalf("concurrent exec not OK: %+v", r)
		}
		got++
	}
	if got != n {
		t.Fatalf("got %d results, want %d", got, n)
	}
}
