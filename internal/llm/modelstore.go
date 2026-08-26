package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ModelRecord describes one locally pulled GGUF model file.
type ModelRecord struct {
	Name    string    `json:"name"`
	RepoID  string    `json:"repo_id"`
	File    string    `json:"file"`
	Bytes   int64     `json:"bytes"`
	SHA256  string    `json:"sha256"`
	AddedAt time.Time `json:"added_at"`
}

// hfFileEntry is one sibling entry from the HF manifest response.
type hfFileEntry struct {
	Path string
	Size int64
}

// ModelStore manages ~/.meept/models: pulled GGUF files plus an index.json
// describing them. Downloads come straight from the HuggingFace resolve
// endpoints over plain HTTPS (no SDK); HF_TOKEN is used as a bearer token
// when set.
type ModelStore struct {
	dir     string
	client  *http.Client
	baseURL string // override for tests; empty = https://huggingface.co

	logDebug func(msg string, args ...any)
}

// OpenModelStore opens (creating if needed) the model store rooted at dir.
func OpenModelStore(dir string) (*ModelStore, error) {
	return openModelStore(dir, http.DefaultClient, "")
}

// OpenModelStoreForTesting is the test seam: injectable HTTP client and hub
// base URL.
func OpenModelStoreForTesting(dir string, client *http.Client, baseURL string) (*ModelStore, error) {
	return openModelStore(dir, client, baseURL)
}

func openModelStore(dir string, client *http.Client, baseURL string) (*ModelStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("modelstore: empty directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("modelstore: create dir: %w", err)
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &ModelStore{
		dir:      dir,
		client:   client,
		baseURL:  baseURL,
		logDebug: func(string, ...any) {},
	}, nil
}

// indexPath returns the path of the store's index.json.
func (s *ModelStore) indexPath() string { return filepath.Join(s.dir, "index.json") }

// readIndex loads all records from index.json; missing file -> empty list.
func (s *ModelStore) readIndex() ([]ModelRecord, error) {
	data, err := os.ReadFile(s.indexPath())
	if os.IsNotExist(err) {
		return []ModelRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("modelstore: read index: %w", err)
	}
	var recs []ModelRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("modelstore: decode index: %w", err)
	}
	return recs, nil
}

// writeIndex atomically persists records (tmp + rename).
func (s *ModelStore) writeIndex(recs []ModelRecord) error {
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return fmt.Errorf("modelstore: encode index: %w", err)
	}
	tmp := s.indexPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("modelstore: write index tmp: %w", err)
	}
	if err := os.Rename(tmp, s.indexPath()); err != nil {
		return fmt.Errorf("modelstore: commit index: %w", err)
	}
	return nil
}

// upsert replaces or appends rec by Name and persists.
func (s *ModelStore) upsert(rec ModelRecord) error {
	recs, err := s.readIndex()
	if err != nil {
		return err
	}
	replaced := false
	for i := range recs {
		if recs[i].Name == rec.Name {
			recs[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		recs = append(recs, rec)
	}
	return s.writeIndex(recs)
}

// List returns all records.
func (s *ModelStore) List() []ModelRecord {
	recs, err := s.readIndex()
	if err != nil {
		s.logDebug("modelstore: index unreadable", "error", err)
		return []ModelRecord{}
	}
	return recs
}

// Get returns the record with the given name.
func (s *ModelStore) Get(name string) (ModelRecord, bool) {
	for _, r := range s.List() {
		if r.Name == name {
			return r, true
		}
	}
	return ModelRecord{}, false
}

// hfManifest queries the HF API for repo file entries.
func (s *ModelStore) hfManifest(ctx context.Context, repoID string) ([]hfFileEntry, error) {
	url := strings.TrimSuffix(s.baseURL, "/") + "/api/models/" + repoID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("modelstore: request: %w", err)
	}
	if tok := os.Getenv("HF_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("modelstore: manifest fetch: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			s.logDebug("modelstore: close body", "error", cerr)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("modelstore: manifest fetch: status %d for %s", resp.StatusCode, repoID)
	}
	var payload struct {
		Siblings []struct {
			RFileName string `json:"rfilename"`
			Size      int64  `json:"size"`
		} `json:"siblings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("modelstore: decode manifest: %w", err)
	}
	out := make([]hfFileEntry, 0, len(payload.Siblings))
	for _, sib := range payload.Siblings {
		out = append(out, hfFileEntry{Path: sib.RFileName, Size: sib.Size})
	}
	return out, nil
}

// pickGGUF selects the single .gguf entry matching quant substring.
// quant=="" requires exactly one .gguf; ambiguity lists candidates.
func pickGGUF(files []hfFileEntry, quant string) (hfFileEntry, error) {
	var matches []hfFileEntry
	for _, f := range files {
		if !strings.HasSuffix(strings.ToLower(f.Path), ".gguf") {
			continue
		}
		if quant == "" || strings.Contains(strings.ToLower(f.Path), strings.ToLower(quant)) {
			matches = append(matches, f)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return hfFileEntry{}, fmt.Errorf("modelstore: no .gguf files in repo")
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, m.Path)
		}
		sort.Strings(names)
		return hfFileEntry{}, fmt.Errorf("modelstore: ambiguous selection (%d gguf candidates): %s",
			len(names), strings.Join(names, ", "))
	}
}

// Pull downloads repoID's selected GGUF into the store. progress may be nil.
// Resumable: an existing <file>.part continues via Range; servers without
// Range support cause a clean restart.
func (s *ModelStore) Pull(ctx context.Context, repoID, quant string, progress func(done, total int64)) (*ModelRecord, error) {
	files, err := s.hfManifest(ctx, repoID)
	if err != nil {
		return nil, err
	}
	entry, err := pickGGUF(files, quant)
	if err != nil {
		return nil, err
	}

	name := filepath.Base(entry.Path)
	finalPath := filepath.Join(s.dir, name)
	partPath := finalPath + ".part"

	if existing, statErr := os.Stat(finalPath); statErr == nil {
		if r, ok := s.Get(name); ok && !s.needsRedownload(name, entry.Size) && existing.Size() == entry.Size {
			rec := r
			return &rec, nil
		}
		// Existing file without a valid record: re-download over it.
		s.removePart(partPath)
	} else if os.IsNotExist(statErr) {
		s.removePart(partPath)
	}

	written, err := s.download(ctx, repoID, entry, partPath, progress)
	if err != nil {
		return nil, err
	}
	sum, err := sha256File(partPath)
	if err != nil {
		return nil, fmt.Errorf("modelstore: hash: %w", err)
	}

	rec := ModelRecord{
		Name:    name,
		RepoID:  repoID,
		File:    finalPath,
		Bytes:   written,
		SHA256:  sum,
		AddedAt: time.Now(),
	}
	if err := os.Rename(partPath, finalPath); err != nil {
		return nil, fmt.Errorf("modelstore: finalize: %w", err)
	}
	if err := s.upsert(rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// removePart best-effort deletes a .part file; failures are non-fatal.
func (s *ModelStore) removePart(partPath string) {
	if rmErr := os.Remove(partPath); rmErr != nil && !os.IsNotExist(rmErr) {
		s.logDebug("modelstore: failed to remove part file", "path", partPath, "error", rmErr)
	}
}

// needsRedownload reports whether the recorded file is missing, resized, or
// hash-mismatched versus the index.
func (s *ModelStore) needsRedownload(name string, hubSize int64) bool {
	rec, ok := s.Get(name)
	if !ok {
		return true
	}
	st, err := os.Stat(rec.File)
	if err != nil {
		return true
	}
	if hubSize > 0 && st.Size() != hubSize {
		return true
	}
	sum, err := sha256File(rec.File)
	if err != nil || sum != rec.SHA256 {
		return true
	}
	return false
}

// download streams entry into partPath with Range resume support.
func (s *ModelStore) download(ctx context.Context, repoID string, f hfFileEntry, partPath string, progress func(done, total int64)) (int64, error) {
	var offset int64
	if st, err := os.Stat(partPath); err == nil {
		offset = st.Size()
	}

	url := strings.TrimSuffix(s.baseURL, "/") + "/" + repoID + "/resolve/main/" + f.Path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("modelstore: request: %w", err)
	}
	if tok := os.Getenv("HF_TOKEN"); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("modelstore: download: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			s.logDebug("modelstore: close body", "error", cerr)
		}
	}()

	switch {
	case resp.StatusCode == http.StatusPartialContent:
		// resume as planned
	case resp.StatusCode == http.StatusOK && offset > 0:
		// server ignored Range; restart from scratch
		offset = 0
		s.removePart(partPath)
	case resp.StatusCode == http.StatusOK:
		// fresh download
	default:
		return 0, fmt.Errorf("modelstore: download: status %d for %s", resp.StatusCode, f.Path)
	}

	resuming := resp.StatusCode == http.StatusPartialContent && offset > 0
	flag := os.O_CREATE | os.O_WRONLY
	if resuming {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
		offset = 0
	}
	out, err := os.OpenFile(partPath, flag, 0o600)
	if err != nil {
		return 0, fmt.Errorf("modelstore: open part: %w", err)
	}

	hasher := sha256.New()
	if resuming {
		existing, rerr := os.ReadFile(partPath)
		if rerr == nil {
			if _, werr := hasher.Write(existing); werr != nil {
				// sha256.hash.Write never returns an error; guard anyway.
				s.logDebug("modelstore: hash existing part", "error", werr)
			}
		}
	}
	n, err := io.Copy(io.MultiWriter(out, hasher), resp.Body)
	if cerr := out.Close(); cerr != nil && err == nil {
		err = cerr
	}
	if err != nil {
		s.removePart(partPath)
		return 0, fmt.Errorf("modelstore: download body: %w", err)
	}
	total := offset + n
	if f.Size > 0 && total != f.Size {
		s.removePart(partPath)
		return 0, fmt.Errorf("modelstore: size mismatch for %s: got %d want %d", f.Path, total, f.Size)
	}
	if progress != nil {
		progress(total, total)
	}
	return total, nil
}

// sha256File computes the hex sha256 of a file streaming its contents.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "modelstore: close %s: %v\n", path, cerr)
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
