package clipstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testKey(seed string) string {
	return ClipKey(seed, "v", "m", "voice", nil)
}

func testMeta(key string, n int) Meta {
	return Meta{
		Key:        key,
		Text:       "Just a sec.",
		Vendor:     "qwen3",
		Model:      "qwen3-tts-flash",
		Voice:      "CloneVoice",
		SampleRate: 24000,
		Bytes:      int64(n),
		CreatedAt:  time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC),
	}
}

// TestNewStoreUsesMeeptPath pins the Contract C2 dir: MEEPT_HOME-aware
// config.MeeptPath("voice_clips").
func TestNewStoreUsesMeeptPath(t *testing.T) {
	t.Setenv("MEEPT_HOME", t.TempDir())
	s := NewStore()
	want := filepath.Join(os.Getenv("MEEPT_HOME"), "voice_clips")
	if s.Dir() != want {
		t.Errorf("NewStore dir = %s, want %s", s.Dir(), want)
	}
}

// TestHasGetNeverCreateDir pins plan R4: read paths leave the
// filesystem untouched when no Put ever happened.
func TestHasGetNeverCreateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "voice_clips")
	s := NewStoreAt(dir)
	key := testKey("a")

	if s.Has(key) {
		t.Error("Has on empty store = true")
	}
	if _, _, err := s.Get(key); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Get on empty store err = %v, want os.ErrNotExist", err)
	}
	if keys, err := s.ListKeys(); err != nil || keys != nil {
		t.Errorf("ListKeys on missing dir = %v, %v; want nil, nil", keys, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("read paths created the store dir — R4 gate violated")
	}
}

func TestPutCreatesDirAndRoundTrips(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "voice_clips")
	s := NewStoreAt(dir)
	pcm := bytes.Repeat([]byte{0x01, 0x02}, 320) // 640 bytes = 20ms@16k
	key := testKey("roundtrip")

	if err := s.Put(key, testMeta(key, len(pcm)), bytes.NewReader(pcm)); err != nil {
		t.Fatalf("Put: %v", err)
	}

	st, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Put did not create dir: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}

	pcmPath := filepath.Join(dir, key+".pcm")
	metaPath := filepath.Join(dir, key+".json")
	for _, p := range []string{pcmPath, metaPath} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s missing after Put: %v", p, err)
		}
		if perm := st.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s mode = %o, want 600", filepath.Base(p), perm)
		}
	}

	if !s.Has(key) {
		t.Fatal("Has after Put = false")
	}
	rc, meta, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read pcm: %v", err)
	}
	if !bytes.Equal(got, pcm) {
		t.Errorf("pcm roundtrip mismatch (%d vs %d bytes)", len(got), len(pcm))
	}
	if meta.Text != "Just a sec." || meta.SampleRate != 24000 || meta.Bytes != int64(len(pcm)) {
		t.Errorf("meta roundtrip mismatch: %+v", meta)
	}
	if meta.Key != key {
		t.Errorf("meta key = %s, want %s", meta.Key, key)
	}
}

// TestPutStampsBytes: Put rewrites meta.Bytes from the bytes actually
// written, so a zero-valued caller meta never poisons Has. (A nonzero
// mismatching Bytes is REJECTED as a truncated stream, not stamped over —
// see TestPutRejectsTruncatedStream.)
func TestPutStampsBytes(t *testing.T) {
	s := NewStoreAt(filepath.Join(t.TempDir(), "voice_clips"))
	pcm := []byte{0xAA, 0xBB, 0xCC}
	key := testKey("stamped")
	if err := s.Put(key, testMeta(key, 0), bytes.NewReader(pcm)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !s.Has(key) {
		t.Fatal("Has = false after Put stamped meta")
	}
	_, meta, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if meta.Bytes != 3 {
		t.Errorf("meta.Bytes = %d, want stamped 3", meta.Bytes)
	}
}

// TestPutRejectsTruncatedStream: a reader that ends short of meta.Bytes
// is an error and leaves NO cache entry (never a corrupt hit).
func TestPutRejectsTruncatedStream(t *testing.T) {
	s := NewStoreAt(filepath.Join(t.TempDir(), "voice_clips"))
	key := testKey("trunc")
	r := io.LimitReader(strings.NewReader("0123456789"), 4)
	err := s.Put(key, testMeta(key, 10), r)
	if err == nil {
		t.Fatal("Put of truncated stream = nil error")
	}
	if s.Has(key) {
		t.Error("truncated Put produced a cache hit")
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), key+".json")); !os.IsNotExist(err) {
		t.Error("meta left behind by failed Put")
	}
}

// TestGetCorruptLengthDeletesBoth: a short pcm vs meta.bytes is a miss —
// Get deletes both files and reports os.ErrNotExist (Contract C2).
func TestGetCorruptLengthDeletesBoth(t *testing.T) {
	s := NewStoreAt(filepath.Join(t.TempDir(), "voice_clips"))
	key := testKey("corrupt")
	if err := s.Put(key, testMeta(key, 8), bytes.NewReader([]byte("12345678"))); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// truncate the pcm behind the store's back
	pcmPath := filepath.Join(s.Dir(), key+".pcm")
	if err := os.WriteFile(pcmPath, []byte("123"), 0o600); err != nil {
		t.Fatal(err)
	}

	if s.Has(key) {
		t.Error("Has = true for length-mismatched clip")
	}
	_, _, err := s.Get(key)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Get err = %v, want os.ErrNotExist", err)
	}
	for _, p := range []string{pcmPath, filepath.Join(s.Dir(), key+".json")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("corrupt file %s not deleted by Get", filepath.Base(p))
		}
	}
}

// TestGetUnparseableMetaMisses: junk meta JSON = miss + pair cleanup.
func TestGetUnparseableMetaMisses(t *testing.T) {
	s := NewStoreAt(filepath.Join(t.TempDir(), "voice_clips"))
	key := testKey("junkmeta")
	if err := s.Put(key, testMeta(key, 4), bytes.NewReader([]byte("abcd"))); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), key+".json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.Has(key) {
		t.Error("Has = true with unparseable meta")
	}
	if _, _, err := s.Get(key); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Get err = %v, want os.ErrNotExist", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), key+".pcm")); !os.IsNotExist(err) {
		t.Error("pcm of unparseable-meta entry not deleted")
	}
}

// TestHasMissingPcm: meta alone is not a hit.
func TestHasMissingPcm(t *testing.T) {
	s := NewStoreAt(filepath.Join(t.TempDir(), "voice_clips"))
	key := testKey("orphanmeta")
	if err := os.MkdirAll(s.Dir(), 0o700); err != nil {
		t.Fatal(err)
	}
	// write meta directly (bypass Put)
	m := testMeta(key, 4)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), key+".json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if s.Has(key) {
		t.Error("Has = true with no pcm file")
	}
	if _, _, err := s.Get(key); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Get err = %v, want os.ErrNotExist", err)
	}
}

// TestRejectsPathTraversalKey: keys are sha256 hex only — "../../evil"
// must never escape the store dir.
func TestRejectsPathTraversalKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "voice_clips")
	s := NewStoreAt(dir)
	bad := "../../escape"
	if err := s.Put(bad, testMeta(bad, 1), strings.NewReader("x")); err == nil {
		t.Error("Put accepted a non-hex key")
	}
	if s.Has(bad) {
		t.Error("Has accepted a non-hex key")
	}
	if _, _, err := s.Get(bad); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Get bad key err = %v, want os.ErrNotExist", err)
	}
}

func TestListKeysSkipsCorruptAndJunk(t *testing.T) {
	s := NewStoreAt(filepath.Join(t.TempDir(), "voice_clips"))
	good := testKey("good")
	bad := testKey("bad")
	if err := s.Put(good, testMeta(good, 4), bytes.NewReader([]byte("abcd"))); err != nil {
		t.Fatalf("Put good: %v", err)
	}
	if err := s.Put(bad, testMeta(bad, 4), bytes.NewReader([]byte("abcd"))); err != nil {
		t.Fatalf("Put bad: %v", err)
	}
	// corrupt the bad entry + drop junk files
	if err := os.WriteFile(filepath.Join(s.Dir(), bad+".pcm"), []byte("ab"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), "zz-not-a-key.pcm"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	keys, err := s.ListKeys()
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if len(keys) != 1 || keys[0] != good {
		t.Errorf("ListKeys = %v, want [%s]", keys, good)
	}
}

// TestConcurrentPutSameKeyNoMutexIO: the store has no mutex; concurrent
// Puts of one key must leave a valid entry (last rename wins) with no
// torn reads.
func TestConcurrentPutSameKeyNoMutexIO(t *testing.T) {
	s := NewStoreAt(filepath.Join(t.TempDir(), "voice_clips"))
	key := testKey("race")
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pcm := bytes.Repeat([]byte{byte(i)}, 64)
			if err := s.Put(key, testMeta(key, 64), bytes.NewReader(pcm)); err != nil {
				t.Errorf("concurrent Put %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	rc, meta, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get after concurrent Puts: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if int64(len(got)) != meta.Bytes || len(got) != 64 {
		t.Errorf("torn entry: %d bytes, meta %d", len(got), meta.Bytes)
	}
	// no temp files left behind
	ents, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}
