package clipstore

// Contract C2 (FROZEN, quoted verbatim from
// docs/plans/20261113-filler-clips-and-context-warm/master.md):
//
//	dir:  config.MeeptPath("voice_clips")            // honors MEEPT_HOME (internal/config/home.go:42)
//	pcm:  <clipKey>.pcm                                raw s16le PCM at the vendor's native output rate (24000 for qwen3; child resamples via existing resample.go path)
//	meta: <clipKey>.json  {key, text, vendor, model, voice, sample_rate, bytes, created_at}
//
//	"No index file; the store is content-addressed. Presence of a valid
//	<key>.pcm with a matching meta `bytes` is the ONLY cache-hit condition.
//	Corrupt/short file = miss (delete, regenerate)."
//
//	"Has/Get never create the dir" (pins the R4 "no sidecar, no dir" gate).
//
// 00T gate leaf: these tests pin the contract independently of the leaf-01
// unit suite (which shares package clipstore). Where the two overlap they
// must agree byte-for-byte on the JSON key names below.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// C2-1: the meta JSON must carry EXACTLY the C2 key set — {key, text,
// vendor, model, voice, sample_rate, bytes, created_at} — and no index
// file exists.
func TestClipGateMetaJSONShapeIsC2(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "voice_clips")
	s := NewStoreAt(dir)
	key := ClipKey("Just a sec.", "qwen3", "m", "v", nil)
	pcm := bytes.Repeat([]byte{0x7f, 0x00}, 160) // 320 bytes
	meta := Meta{
		Key:        key,
		Text:       "Just a sec.",
		Vendor:     "qwen3",
		Model:      "m",
		Voice:      "v",
		SampleRate: 24000,
		Bytes:      int64(len(pcm)),
	}
	if err := s.Put(key, meta, bytes.NewReader(pcm)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil {
		t.Fatalf("meta file: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("meta is not JSON: %v", err)
	}
	for _, want := range []string{"key", "text", "vendor", "model", "voice", "sample_rate", "bytes", "created_at"} {
		if _, ok := fields[want]; !ok {
			t.Errorf("meta JSON missing C2 key %q (have %v)", want, keysOf(fields))
		}
	}
	// Content-addressed: no index/manifest file beside the pairs (C2).
	for _, banned := range []string{"index.json", "manifest.json", "clips.json"} {
		if _, err := os.Stat(filepath.Join(dir, banned)); err == nil {
			t.Errorf("store carries an index file %s — C2 pins content-addressing with NO index", banned)
		}
	}
}

// C2-2: cache-hit condition is pcm EXISTENCE + meta.bytes LENGTH MATCH —
// a short (truncated) pcm is a MISS and the pair is DELETED (regeneration
// path), never served.
func TestClipGateShortPcmIsMissAndDeleted(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "voice_clips")
	s := NewStoreAt(dir)
	key := ClipKey("One moment.", "qwen3", "m", "v", nil)
	pcm := bytes.Repeat([]byte{0x01}, 640)
	if err := s.Put(key, Meta{Key: key, Text: "One moment.", SampleRate: 24000, Bytes: int64(len(pcm))}, bytes.NewReader(pcm)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Simulate a corrupt/truncated clip: rewrite the pcm short.
	pcmPath := filepath.Join(dir, key+".pcm")
	if err := os.WriteFile(pcmPath, []byte("trunc"), 0o600); err != nil {
		t.Fatalf("truncate pcm: %v", err)
	}
	if s.Has(key) {
		t.Fatal("Has = true on a short pcm — C2 hit condition violated")
	}
	rc, _, err := s.Get(key)
	if err == nil {
		_ = rc.Close() //nolint:errcheck // failure path
		t.Fatal("Get served a corrupt clip — C2 pins delete+miss")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Get err = %v, want os.ErrNotExist (miss shape)", err)
	}
	if _, err := os.Stat(pcmPath); !os.IsNotExist(err) {
		t.Errorf("corrupt pcm not deleted (C2: corrupt/short file = miss (delete, regenerate))")
	}
	if _, err := os.Stat(filepath.Join(dir, key+".json")); !os.IsNotExist(err) {
		t.Errorf("stale meta not deleted alongside the corrupt pcm")
	}
}

// C2-3: NewStore resolves the dir through config.MeeptPath("voice_clips")
// — honors MEEPT_HOME verbatim (the R4 gate's filesystem target).
func TestClipGateNewStoreResolvesMeeptHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MEEPT_HOME", home)
	s := NewStore()
	if want := filepath.Join(home, "voice_clips"); s.Dir() != want {
		t.Fatalf("store dir = %s, want %s (C2: config.MeeptPath(\"voice_clips\"))", s.Dir(), want)
	}
}

// C2-4: Has/Get on an absent dir create NOTHING (R4 pin reused at the
// store level: the daemon-side gate proves the dir stays absent when the
// sidecar never ran; this proves the store itself cannot leak the dir on
// read paths).
func TestClipGateReadPathsNeverCreateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "voice_clips")
	s := NewStoreAt(dir)
	if s.Has(ClipKey("x", "v", "m", "voice", nil)) {
		t.Fatal("Has true on empty store")
	}
	rc, _, err := s.Get(ClipKey("x", "v", "m", "voice", nil))
	if err == nil {
		_ = rc.Close() //nolint:errcheck // failure path
	}
	if _, err := s.ListKeys(); err != nil {
		t.Errorf("ListKeys on missing dir must not error: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("read paths created the store dir")
	}
}

// C2-5: a valid hit round-trips EXACT bytes through Get (raw s16le, no
// framing — no PCM ever rides C1 frames; the store is the only carrier).
func TestClipGateHitRoundTripsRawPCM(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "voice_clips")
	s := NewStoreAt(dir)
	key := ClipKey("Talk soon.", "qwen3", "m", "v", nil)
	pcm := make([]byte, 2*4800) // 200 ms at 24 kHz s16le
	for i := 0; i < len(pcm); i += 2 {
		pcm[i] = byte(i)        //nolint:gosec // G602: i < len(pcm) by the loop bound
		pcm[i+1] = byte(i >> 8) //nolint:gosec // G602: len(pcm) is even, so i+1 stays in bounds
	}
	if err := s.Put(key, Meta{Key: key, Text: "Talk soon.", Vendor: "qwen3", Model: "m", Voice: "v", SampleRate: 24000, Bytes: int64(len(pcm))}, bytes.NewReader(pcm)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	rc, meta, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // test sink
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, pcm) {
		t.Fatalf("pcm bytes differ (%d vs %d)", len(got), len(pcm))
	}
	if meta.SampleRate != 24000 || meta.Text != "Talk soon." {
		t.Errorf("meta = %+v, want sample_rate 24000 + text echoed", meta)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
