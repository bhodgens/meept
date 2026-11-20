package clipstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/caimlas/meept/internal/config"
)

// ErrEmptyClip is the typed refusal for a zero-length clip payload: a
// failed/empty TTS render produces no audio, and a zero-length clip is
// NOT a valid cached clip (owner ruling, bughunt 2026-11-19) - serving
// it would play SILENCE as a cache hit. Put refuses it so the render
// answers clip_ready ok:false and the say lane falls through to live
// synth; callers check with errors.Is.
var ErrEmptyClip = errors.New("clipstore: zero-length clip payload")

// layout constants pinned by Contract C2.
const (
	dirName    = "voice_clips"
	pcmExt     = ".pcm"
	metaExt    = ".json"
	dirMode    = 0o700 // store dir: audio of a personal clone voice
	fileMode   = 0o600 // clip + meta files, same posture
	keyHexLen  = 64    // sha256 hex
	tmpPattern = "*.pcm.tmp*"
)

// Store is a content-addressed cache of pre-rendered speech clips under
// $MEEPT_HOME/voice_clips/ (Contract C2). It is safe for concurrent use:
// the store is stateless over the filesystem (no index, no cache of dir
// existence) and every write is atomic (temp file + rename), so there is
// no mutex — and per the mutexio posture, no lock may ever be held across
// the file I/O below.
type Store struct {
	dir string
}

// NewStore returns a Store rooted at config.MeeptPath("voice_clips"),
// honoring MEEPT_HOME (internal/config/home.go). It does NOT create the
// directory: creation is gated on the first successful Put so that a
// deployment with no voice sidecar never writes anything (plan R4).
func NewStore() *Store {
	return &Store{dir: config.MeeptPath(dirName)}
}

// NewStoreAt returns a Store rooted at an explicit dir. Used by tests and
// by callers that resolve the path themselves; NewStore is the production
// constructor.
func NewStoreAt(dir string) *Store {
	return &Store{dir: dir}
}

// Dir returns the store directory path (does not create it).
func (s *Store) Dir() string { return s.dir }

// Has reports a cache hit: meta+pcm exist AND the pcm length equals the
// meta bytes field AND the clip carries audio (meta.Bytes > 0 and the
// pcm size > 0 - a zero-length clip is never a valid cached clip, it
// would play silence). It NEVER creates the directory and never deletes
// anything — a corrupt entry reads as a miss here and is only cleaned up
// by Get (or overwritten by Put).
func (s *Store) Has(key string) bool {
	if !validKey(key) {
		return false
	}
	meta, status := s.loadMeta(key)
	if status != metaOK || meta.Bytes <= 0 {
		return false
	}
	size, err := s.pcmSize(key)
	return err == nil && size > 0 && size == meta.Bytes
}

// Get opens the validated pcm stream for a key. On a length/parse
// mismatch (corrupt or truncated clip) it deletes both files, treating
// the entry as a miss, and returns an error wrapping os.ErrNotExist so
// callers can errors.Is on it. A key that was never stored returns
// os.ErrNotExist without touching the filesystem.
func (s *Store) Get(key string) (io.ReadCloser, Meta, error) {
	if !validKey(key) {
		return nil, Meta{}, fmt.Errorf("clipstore: invalid key %q: %w", key, os.ErrNotExist)
	}
	meta, status := s.loadMeta(key)
	if status == metaCorrupt {
		// Parse mismatch = corrupt entry (leaf 01): delete both, miss.
		s.removePair(key)
		return nil, Meta{}, fmt.Errorf("clipstore: clip %s meta corrupt: %w", key, os.ErrNotExist)
	}
	if status == metaAbsent {
		return nil, Meta{}, fmt.Errorf("clipstore: clip %s not cached: %w", key, os.ErrNotExist)
	}
	if meta.Bytes <= 0 {
		// Zero-length entry (legacy failed render): not a valid clip -
		// corrupt, delete both files like the mismatch path below.
		s.removePair(key)
		return nil, Meta{}, fmt.Errorf("clipstore: clip %s corrupt (zero-length clip): %w", key, os.ErrNotExist)
	}
	pcmPath := filepath.Join(s.dir, key+pcmExt)
	f, err := os.Open(pcmPath)
	if err != nil {
		if os.IsNotExist(err) {
			// meta without pcm: orphaned sidecar, drop it (miss path).
			s.removePair(key)
			return nil, Meta{}, fmt.Errorf("clipstore: clip %s not cached: %w", key, os.ErrNotExist)
		}
		return nil, Meta{}, err
	}
	size, err := f.Stat()
	if err != nil {
		closeIgnoring(f)
		return nil, Meta{}, err
	}
	if size.Size() != meta.Bytes {
		// Corrupt/short file = miss (Contract C2): delete both, regenerate.
		closeIgnoring(f)
		s.removePair(key)
		return nil, Meta{}, fmt.Errorf("clipstore: clip %s corrupt (pcm %d != meta %d bytes): %w",
			key, size.Size(), meta.Bytes, os.ErrNotExist)
	}
	if size.Size() <= 0 {
		// Defensive twin of the meta check above: a zero-byte pcm is
		// never a valid clip even if a future producer stamps Bytes=0.
		closeIgnoring(f)
		s.removePair(key)
		return nil, Meta{}, fmt.Errorf("clipstore: clip %s corrupt (zero-length clip): %w", key, os.ErrNotExist)
	}
	return f, meta, nil
}

// Put atomically stores a clip: pcm is streamed to a temp file in the
// store dir, synced, and renamed to <key>.pcm; the meta JSON follows the
// same temp+rename path. The dir is created (0700) ONLY here, on first
// write. meta.Bytes is checked against the bytes actually written: a
// short read is discarded as an error, never a corrupt cache entry. A
// ZERO-LENGTH payload is refused with ErrEmptyClip before any filesystem
// write: an empty render is not a clip, and caching it would serve
// silence as a hit (owner ruling, bughunt 2026-11-19).
//
// Put is race-safe without a mutex: two concurrent Puts for one key write
// distinct temp files and the last rename wins, always with a complete
// pcm before its meta lands (meta is written after the pcm rename).
func (s *Store) Put(key string, meta Meta, pcm io.Reader) error {
	if !validKey(key) {
		return fmt.Errorf("clipstore: invalid key %q", key)
	}
	if pcm == nil {
		return fmt.Errorf("clipstore: put %s: nil pcm reader", key)
	}
	if meta.Bytes < 0 {
		return fmt.Errorf("clipstore: put %s: negative meta bytes %d", key, meta.Bytes)
	}
	if err := s.ensureDir(); err != nil {
		return err
	}

	pcmPath := filepath.Join(s.dir, key+pcmExt)
	tmp, err := os.CreateTemp(s.dir, key+tmpPattern)
	if err != nil {
		return fmt.Errorf("clipstore: create temp for %s: %w", key, err)
	}
	tmpName := tmp.Name()
	defer func() { removeIgnoring(tmpName) }() // no-op after successful rename

	written, err := io.Copy(tmp, pcm)
	if err != nil {
		closeIgnoring(tmp)
		return fmt.Errorf("clipstore: write temp for %s: %w", key, err)
	}
	if written == 0 {
		// Zero-length render: refuse BEFORE any rename so no cache
		// entry (pcm or meta) ever lands for a silent clip. The temp
		// file is removed by the deferred cleanup; the store dir may
		// have been created above, which is benign (R4 only pins that
		// READ paths never create it).
		closeIgnoring(tmp)
		return fmt.Errorf("clipstore: put %s: %w", key, ErrEmptyClip)
	}
	if meta.Bytes > 0 && written != meta.Bytes {
		closeIgnoring(tmp)
		return fmt.Errorf("clipstore: put %s: pcm truncated (%d written, meta says %d)",
			key, written, meta.Bytes)
	}
	if err := tmp.Sync(); err != nil {
		closeIgnoring(tmp)
		return fmt.Errorf("clipstore: sync temp for %s: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("clipstore: close temp for %s: %w", key, err)
	}
	if err := os.Chmod(tmpName, fileMode); err != nil {
		return fmt.Errorf("clipstore: chmod temp for %s: %w", key, err)
	}
	if err := os.Rename(tmpName, pcmPath); err != nil {
		return fmt.Errorf("clipstore: rename pcm for %s: %w", key, err)
	}

	// Stamp the authoritative values: the key must match the file names,
	// and bytes must match what actually landed on disk.
	meta.Key = key
	meta.Bytes = written
	if err := s.putMeta(key, meta); err != nil {
		// A clip without readable meta is a permanent miss by contract;
		// drop the orphan so regeneration starts clean.
		removeIgnoring(pcmPath)
		return err
	}
	return nil
}

// ListKeys returns every currently-cached clip key (files that pass the
// Has validation). Keys with corrupt/short entries are skipped, not
// deleted — the reload diff (leaf 02) treats a skipped key as a miss and
// Put overwrites it. Returns nil, nil when the store dir does not exist
// yet (nothing has ever been generated — the R4 posture).
func (s *Store) ListKeys() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("clipstore: list keys: %w", err)
	}
	var keys []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, pcmExt) {
			continue
		}
		key := strings.TrimSuffix(name, pcmExt)
		if !validKey(key) {
			continue
		}
		if s.Has(key) {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// putMeta writes the meta JSON sidecar via the same temp+rename path.
func (s *Store) putMeta(key string, meta Meta) error {
	data, err := json.Marshal(&meta)
	if err != nil {
		return fmt.Errorf("clipstore: marshal meta for %s: %w", key, err)
	}
	metaPath := filepath.Join(s.dir, key+metaExt)
	tmp, err := os.CreateTemp(s.dir, key+metaExt+".tmp*")
	if err != nil {
		return fmt.Errorf("clipstore: create meta temp for %s: %w", key, err)
	}
	tmpName := tmp.Name()
	defer func() { removeIgnoring(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		closeIgnoring(tmp)
		return fmt.Errorf("clipstore: write meta temp for %s: %w", key, err)
	}
	if err := tmp.Sync(); err != nil {
		closeIgnoring(tmp)
		return fmt.Errorf("clipstore: sync meta temp for %s: %w", key, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("clipstore: close meta temp for %s: %w", key, err)
	}
	if err := os.Chmod(tmpName, fileMode); err != nil {
		return fmt.Errorf("clipstore: chmod meta temp for %s: %w", key, err)
	}
	if err := os.Rename(tmpName, metaPath); err != nil {
		return fmt.Errorf("clipstore: rename meta for %s: %w", key, err)
	}
	return nil
}

// loadMeta reads <key>.json. status reports the three states the
// contract distinguishes: metaAbsent (never stored — plain miss),
// metaCorrupt (file exists but unparseable/nonsensical — delete pair on
// Get), metaOK.
type metaStatus int

const (
	metaAbsent metaStatus = iota
	metaCorrupt
	metaOK
)

func (s *Store) loadMeta(key string) (Meta, metaStatus) {
	data, err := os.ReadFile(filepath.Join(s.dir, key+metaExt))
	if err != nil {
		if os.IsNotExist(err) {
			return Meta{}, metaAbsent
		}
		// Unreadable (permissions etc): treat as corrupt — Get's
		// regenerate path clears it, Put overwrites.
		return Meta{}, metaCorrupt
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil || meta.Bytes < 0 {
		return Meta{}, metaCorrupt
	}
	return meta, metaOK
}

// pcmSize stats <key>.pcm without opening it for reads.
func (s *Store) pcmSize(key string) (int64, error) {
	st, err := os.Stat(filepath.Join(s.dir, key+pcmExt))
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

// removePair deletes both clip files, ignoring not-exist. Best-effort
// cleanup on a detected corrupt entry (Get's miss path); a failure here
// only means the next Get re-detects and retries.
func (s *Store) removePair(key string) {
	removeIgnoring(filepath.Join(s.dir, key+pcmExt))
	removeIgnoring(filepath.Join(s.dir, key+metaExt))
}

// ensureDir creates the store dir (0700) if absent. Called ONLY from Put
// — Has/Get/ListKeys never create it, which pins the R4 "no sidecar, no
// dir" gate: a daemon that only reads (or never starts the child) leaves
// the filesystem untouched.
func (s *Store) ensureDir() error {
	if err := os.MkdirAll(s.dir, dirMode); err != nil {
		return fmt.Errorf("clipstore: create dir %s: %w", s.dir, err)
	}
	// MkdirAll honors umask for intermediates; force the final dir's mode
	// so a permissive umask never widens a personal-clone audio store.
	if err := os.Chmod(s.dir, dirMode); err != nil {
		return fmt.Errorf("clipstore: chmod dir %s: %w", s.dir, err)
	}
	return nil
}

// validKey guards against path traversal and junk filenames: a clip key
// is exactly 64 lowercase hex chars (sha256 output).
func validKey(key string) bool {
	if len(key) != keyHexLen {
		return false
	}
	if _, err := hex.DecodeString(key); err != nil {
		return false
	}
	return key == strings.ToLower(key)
}

// refHashHex is the inner hex(sha256(ref)) of Contract C1, exposed for
// tests that reconstruct the pre-image byte-exactly.
func refHashHex(refHash []byte) string {
	sum := sha256.Sum256(refHash)
	return hex.EncodeToString(sum[:])
}

// closeIgnoring closes a handle on a cleanup path where the fd is being
// discarded either way: the caller already carries the real error, or the
// entry degrades to a miss-by-contract, so a close failure is benign and
// explicitly swallowed (the store has no logger).
func closeIgnoring(c io.Closer) {
	if err := c.Close(); err != nil {
		return // benign: see doc comment
	}
}

// removeIgnoring is the remove-path twin of closeIgnoring: best-effort
// cleanup where the post-state (file gone, or still gone next time) is
// all the contract cares about.
func removeIgnoring(path string) {
	if err := os.Remove(path); err != nil {
		return // benign: best-effort cleanup, retried by the next miss path
	}
}
