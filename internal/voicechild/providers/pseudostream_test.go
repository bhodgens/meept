package providers

// Gate tests (leaf 00T, features F2/F4, contracts C2/C5). The package must
// stay compilable before leaf 02 lands newPseudoStreamSTT / STTBaseFactory,
// so the missing-symbol gate (TestPseudoStreamEmitsPartialsThenFinal) probes
// the package source via go/parser and names each absent contract symbol;
// it turns GREEN with zero edits once the decorator lands. The guardrail
// (TestNewSTTDefaultNoDecorator) uses only today's symbols and passes now.

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voicechild"
)

// TestNewSTTDefaultNoDecorator (F4 guardrail): with no partial interval
// configured (the zero value), NewSTT must return the BASE provider itself,
// never a decorator wrapper. This passes today and must stay green.
func TestNewSTTDefaultNoDecorator(t *testing.T) {
	stt, err := NewSTT(ProviderConfig{
		Vendor: "rumble", Model: "Reson8/Rumble-2-Flash",
		BaseURL: "http://127.0.0.1:1", SampleRate: 16000,
	}, "")
	if err != nil {
		t.Fatalf("NewSTT: %v", err)
	}
	if _, ok := stt.(*RumbleSTT); !ok {
		t.Fatalf("NewSTT returned %T, want *RumbleSTT (no decorator on the legacy/zero-interval path)", stt)
	}
}

// TestNewSTTNativeNoDecorator (F3/C4): with a positive PartialIntervalMs the
// pseudo-stream decorator still wraps batch vendors, but a NATIVE-partial
// vendor such as rumble must be returned unwrapped (base *RumbleSTT), or the
// call would double-partial. RED until leaf 02 adds the nativeStreamingVendor
// guard to NewSTT (today NewSTT wraps on PartialIntervalMs > 0 regardless).
func TestNewSTTNativeNoDecorator(t *testing.T) {
	stt, err := NewSTT(ProviderConfig{
		Vendor: "rumble", Model: "Reson8/Rumble-2-Flash",
		BaseURL: "http://127.0.0.1:1", SampleRate: 16000,
		PartialIntervalMs: 50,
	}, "")
	if err != nil {
		t.Fatalf("NewSTT: %v", err)
	}
	if _, ok := stt.(*PseudoStreamSTT); ok {
		t.Fatalf("NewSTT wrapped the native-partial rumble vendor in *PseudoStreamSTT (double-partial); want base *RumbleSTT")
	}
	if _, ok := stt.(*RumbleSTT); !ok {
		t.Fatalf("NewSTT returned %T, want *RumbleSTT (native vendor, no decorator)", stt)
	}
}

// ---- source-level gate helpers (keep the package compiling pre-leaf-02) ----

func gateParseProviders(t *testing.T) map[string]*ast.File {
	t.Helper()
	fset := token.NewFileSet()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list providers package source: %v", err)
	}
	out := make(map[string]*ast.File)
	for _, name := range matches {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse providers package source %s: %v", name, err)
		}
		out[name] = f
	}
	if len(out) == 0 {
		t.Fatal("no provider source files found to gate")
	}
	return out
}

func gateFindFunc(t *testing.T, files map[string]*ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
				return fd
			}
		}
	}
	return nil
}

func gateFindType(t *testing.T, files map[string]*ast.File, name string) *ast.TypeSpec {
	t.Helper()
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.Name == name {
					return ts
				}
			}
		}
	}
	return nil
}

func gateExprText(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return gateExprText(x.X) + "." + x.Sel.Name
	case *ast.StarExpr:
		return "*" + gateExprText(x.X)
	case *ast.FuncType:
		return "func"
	case *ast.ArrayType:
		return "[]" + gateExprText(x.Elt)
	default:
		return fmt.Sprintf("%T", e)
	}
}

func gateCheckSignature(t *testing.T, label string, ft *ast.FuncType, wantParams, wantResults int) {
	t.Helper()
	if ft == nil {
		t.Fatalf("%s has no function signature", label)
	}
	if ft.Params == nil || len(ft.Params.List) != wantParams {
		t.Fatalf("%s has %d params, want %d (contract C5)", label, fieldCount(ft.Params), wantParams)
	}
	if ft.Results == nil || len(ft.Results.List) != wantResults {
		t.Fatalf("%s has %d results, want %d (contract C5)", label, resultCount(ft.Results), wantResults)
	}
}

func fieldCount(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	return len(fl.List)
}

func resultCount(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	return len(fl.List)
}

// TestPseudoStreamEmitsPartialsThenFinal (F2/C5): the provider-agnostic
// decorator must exist with the exact C5 shape, NewSTT must opt into it only
// when PartialIntervalMs > 0, and the decorator must emit BOTH a
// Transcript{Final:false} partial and exactly one Transcript{Final:true}.
// RED today: newPseudoStreamSTT / STTBaseFactory / PartialIntervalMs are
// absent. The runtime partial/final timing itself is covered by the
// companion e2e gate (00Te) and the implementing leaf's own tests.
func TestPseudoStreamEmitsPartialsThenFinal(t *testing.T) {
	files := gateParseProviders(t)

	// 1. STTBaseFactory type: func(ProviderConfig, string) (STTProvider, error).
	ts := gateFindType(t, files, "STTBaseFactory")
	if ts == nil {
		t.Fatalf("missing symbol type STTBaseFactory (contract C5 F2): generic pseudo-stream cannot be built")
	}
	ft, ok := ts.Type.(*ast.FuncType)
	if !ok {
		t.Fatalf("STTBaseFactory is %T, want a func type (contract C5)", ts.Type)
	}
	gateCheckSignature(t, "STTBaseFactory", ft, 2, 2)
	if got := gateExprText(ft.Params.List[0].Type); got != "ProviderConfig" {
		t.Errorf("STTBaseFactory param 0 = %s, want ProviderConfig", got)
	}
	if got := gateExprText(ft.Params.List[1].Type); got != "string" {
		t.Errorf("STTBaseFactory param 1 = %s, want string (key)", got)
	}
	if got := gateExprText(ft.Results.List[0].Type); !strings.Contains(got, "STTProvider") {
		t.Errorf("STTBaseFactory result 0 = %s, want STTProvider", got)
	}
	if got := gateExprText(ft.Results.List[1].Type); got != "error" {
		t.Errorf("STTBaseFactory result 1 = %s, want error", got)
	}

	// 2. newPseudoStreamSTT(cfg, key, base) (STTProvider, error).
	fd := gateFindFunc(t, files, "newPseudoStreamSTT")
	if fd == nil {
		t.Fatalf("missing symbol func newPseudoStreamSTT (contract C5 F2): no client-side pseudo-stream")
	}
	gateCheckSignature(t, "newPseudoStreamSTT", fd.Type, 3, 2)
	if got := gateExprText(fd.Type.Params.List[0].Type); got != "ProviderConfig" {
		t.Errorf("newPseudoStreamSTT param 0 = %s, want ProviderConfig", got)
	}
	if got := gateExprText(fd.Type.Params.List[1].Type); got != "string" {
		t.Errorf("newPseudoStreamSTT param 1 = %s, want string (key)", got)
	}
	switch p2 := fd.Type.Params.List[2].Type.(type) {
	case *ast.FuncType: // inline func literal
	case *ast.Ident:
		if p2.Name != "STTBaseFactory" {
			t.Errorf("newPseudoStreamSTT param 2 = %s, want STTBaseFactory/base func", p2.Name)
		}
	default:
		t.Errorf("newPseudoStreamSTT param 2 = %s, want STTBaseFactory/base func", gateExprText(p2))
	}
	if got := gateExprText(fd.Type.Results.List[0].Type); !strings.Contains(got, "STTProvider") {
		t.Errorf("newPseudoStreamSTT result 0 = %s, want STTProvider", got)
	}
	if got := gateExprText(fd.Type.Results.List[1].Type); got != "error" {
		t.Errorf("newPseudoStreamSTT result 1 = %s, want error", got)
	}

	// 3. NewSTT opts in only when PartialIntervalMs > 0 and wraps via the decorator.
	nf := gateFindFunc(t, files, "NewSTT")
	if nf == nil || nf.Body == nil {
		t.Fatalf("missing func NewSTT body (contract C5)")
	}
	seen := make(map[string]bool)
	ast.Inspect(nf.Body, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			seen[id.Name] = true
		}
		return true
	})
	if !seen["PartialIntervalMs"] {
		t.Errorf("NewSTT does not consult PartialIntervalMs - the decorator is not opt-in (contract C5)")
	}
	if !seen["newPseudoStreamSTT"] {
		t.Errorf("NewSTT does not call newPseudoStreamSTT - decorator is not wired (contract C5)")
	}

	// 4. Both a Final:false partial and a Final:true final must be emitted.
	var hasFalse, hasTrue bool
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			kv, ok := n.(*ast.KeyValueExpr)
			if !ok {
				return true
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok || key.Name != "Final" {
				return true
			}
			if id, ok := kv.Value.(*ast.Ident); ok {
				switch id.Name {
				case "false":
					hasFalse = true
				case "true":
					hasTrue = true
				}
			}
			return true
		})
	}
	if !hasFalse {
		t.Errorf("no Transcript{Final:false} partial emission found (contract C5: intermediate partials)")
	}
	if !hasTrue {
		t.Errorf("no Transcript{Final:true} final emission found (contract C5: exactly one final)")
	}
}

// ---- runtime pins (bughunt 2026-11-19 H3/M2) ----

// pseudoFakeSTT is a batch base for the decorator pins: it drains the
// snapshot channel, records the total byte count of every recognition
// pass, and answers one Final:true naming that count ("w<N>"). Empty
// snapshots answer nothing (mirrors a silent utterance).
type pseudoFakeSTT struct {
	mu    sync.Mutex
	sizes []int
}

func (f *pseudoFakeSTT) Stream(_ context.Context, pcm <-chan []byte) (<-chan voicechild.Transcript, error) {
	out := make(chan voicechild.Transcript, 4)
	go func() {
		defer close(out)
		total := 0
		for chunk := range pcm {
			total += len(chunk)
		}
		f.mu.Lock()
		f.sizes = append(f.sizes, total)
		f.mu.Unlock()
		if total > 0 {
			out <- voicechild.Transcript{Text: fmt.Sprintf("w%d", total), Final: true}
		}
	}()
	return out, nil
}

func (f *pseudoFakeSTT) snapshotSizes() []int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]int(nil), f.sizes...)
}

// newPseudoForPin builds the decorator with a 1-hour tick interval so the
// partial ticker NEVER fires during a pin — only kicks, the cap, and pcm
// close drive emissions.
func newPseudoForPin(t *testing.T, fake *pseudoFakeSTT) *PseudoStreamSTT {
	t.Helper()
	p, err := newPseudoStreamSTT(
		ProviderConfig{Vendor: "whisper", SampleRate: 16000, PartialIntervalMs: 3600000},
		"",
		func(ProviderConfig, string) (voicechild.STTProvider, error) { return fake, nil },
	)
	if err != nil {
		t.Fatalf("newPseudoStreamSTT: %v", err)
	}
	ps, ok := p.(*PseudoStreamSTT)
	if !ok {
		t.Fatalf("decorator type = %T, want *PseudoStreamSTT", p)
	}
	return ps
}

// TestPseudoStreamFlushNowKeepsStreaming (H3): the pipeline kicks FlushNow
// on EVERY loud RMS window; a kick must flush-and-keep-streaming, never
// close the transcript channel (the consumer never reopens a cleanly
// closed channel, so the old finalize-and-return deafened the call after
// the first barge-in attempt). Two successive kicks must each deliver a
// Final:true, and the second recognition must see ONLY the post-reset
// bytes (buffer + lastText reset proof).
func TestPseudoStreamFlushNowKeepsStreaming(t *testing.T) {
	fake := &pseudoFakeSTT{}
	ps := newPseudoForPin(t, fake)
	// Unbuffered: each send completes only when the run loop RECEIVES the
	// chunk, and the append finishes before the next select — so a
	// following FlushNow is guaranteed to see the buffered window.
	pcm := make(chan []byte)
	trs := streamOf(t, ps, pcm)

	pcm <- chunkOf(100, loudAmplitude) // 1600 B window
	ps.FlushNow()
	select {
	case tr, ok := <-trs:
		if !ok {
			t.Fatal("stream closed on the FIRST FlushNow kick (H3: kick must keep streaming)")
		}
		if !tr.Final || tr.Text != "w1600" {
			t.Fatalf("first kick: want final w1600, got %+v", tr)
		}
	case <-time.After(streamDrainWait):
		t.Fatal("first FlushNow kick never delivered a final")
	}

	// Second window after the kick: the stream must still be live.
	pcm <- chunkOf(50, loudAmplitude) // 800 B — NOT 2400 B: reset proof
	ps.FlushNow()
	select {
	case tr, ok := <-trs:
		if !ok {
			t.Fatal("stream closed after the first kick: second barge-in attempt went deaf (H3)")
		}
		if !tr.Final || tr.Text != "w800" {
			t.Fatalf("second kick: want final w800 (buffer reset), got %+v", tr)
		}
	case <-time.After(streamDrainWait):
		t.Fatal("second FlushNow kick never delivered a final (stream deaf after first kick, H3)")
	}

	// pcm close still flushes exactly one final (empty allowed after the
	// reset) and THEN closes the channel — unchanged close contract.
	close(pcm)
	got := drainTranscripts(t, trs)
	if len(got) != 1 || !got[0].Final {
		t.Fatalf("pcm close: want exactly one final then close, got %v", got)
	}
}

// TestPseudoStreamMaxWindowCap (M2): every tick re-recognizes the WHOLE
// accumulated buffer, so a never-silent source must hit the 10 s cap
// (320000 B at 16 kHz s16le) and force a finalize-and-reset — memory and
// re-recognition cost stay bounded. The cap flush must arrive WITHOUT a
// pcm close or kick, and no recognition pass may ever see more than the
// cap (640 B chunks land the cap exactly).
func TestPseudoStreamMaxWindowCap(t *testing.T) {
	fake := &pseudoFakeSTT{}
	ps := newPseudoForPin(t, fake)
	pcm := make(chan []byte, 1024)
	trs := streamOf(t, ps, pcm)

	// 500 x 640 B = exactly 320000 B: the cap fires on the last chunk.
	go func() {
		for range 500 {
			pcm <- chunkOf(40, loudAmplitude) // 40 ms @ 8 kHz = 640 B
		}
	}()
	select {
	case tr, ok := <-trs:
		if !ok {
			t.Fatal("stream closed at the window cap (cap must flush-and-reset, not close)")
		}
		if !tr.Final || tr.Text != "w320000" {
			t.Fatalf("cap flush: want final w320000, got %+v", tr)
		}
	case <-time.After(streamDrainWait):
		t.Fatal("320000 B accumulated with no cap flush (M2: unbounded O(n^2) window)")
	}

	// A small tail after the cap recognizes ONLY the tail (reset proof),
	// then pcm close delivers the usual single final.
	pcm <- chunkOf(40, loudAmplitude)
	close(pcm)
	got := drainTranscripts(t, trs)
	if len(got) != 1 || !got[0].Final || got[0].Text != "w640" {
		t.Fatalf("post-cap tail: want one final w640, got %v", got)
	}
	for _, n := range fake.snapshotSizes() {
		if n > pseudoStreamMaxWindowBytes {
			t.Fatalf("recognition pass saw %d bytes (> %d cap): window not bounded (M2)", n, pseudoStreamMaxWindowBytes)
		}
	}
}
