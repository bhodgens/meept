package learning

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// DatasetVersion records metadata about a snapshot of a domain dataset.
type DatasetVersion struct {
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"created_at"`
	Domain       string    `json:"domain"`
	ExampleCount int       `json:"example_count"`
	MD5          string    `json:"md5"`
	Source       string    `json:"source"` // "raw_captures" or "distillation"
}

// CreateSnapshot copies {datasetsDir}/{domain}.jsonl to
// {versionsDir}/{domain}_v{N}.jsonl where N is the next version number,
// computes the MD5 checksum, and appends a DatasetVersion entry to
// {versionsDir}/versions.json.
func CreateSnapshot(domain, datasetsDir, versionsDir string) (*DatasetVersion, error) {
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		return nil, fmt.Errorf("learning: mkdir versions %s: %w", versionsDir, err)
	}

	srcPath := filepath.Join(datasetsDir, domain+".jsonl")
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return nil, fmt.Errorf("learning: open source dataset %s: %w", srcPath, err)
	}
	defer srcFile.Close()

	// Count existing versions for this domain to compute the next version number.
	nextVer, err := nextVersionNumber(domain, versionsDir)
	if err != nil {
		return nil, err
	}

	dstPath := filepath.Join(versionsDir, fmt.Sprintf("%s_v%d.jsonl", domain, nextVer))
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return nil, fmt.Errorf("learning: create versioned snapshot %s: %w", dstPath, err)
	}

	hasher := md5.New()
	exampleCount := 0

	// We need to count lines and compute MD5. Copy through a tee reader and
	// count newlines.
	tee := io.TeeReader(srcFile, dstFile)
	scannerBuf := make([]byte, 32*1024)
	lineCount := 0
	for {
		n, rerr := tee.Read(scannerBuf)
		if n > 0 {
			hasher.Write(scannerBuf[:n])
			// Count newlines in the chunk.
			for _, b := range scannerBuf[:n] {
				if b == '\n' {
					lineCount++
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			dstFile.Close()
			return nil, fmt.Errorf("learning: copy dataset to snapshot: %w", rerr)
		}
	}
	dstFile.Close()
	exampleCount = lineCount

	version := &DatasetVersion{
		Version:      nextVer,
		CreatedAt:    time.Now().UTC(),
		Domain:       domain,
		ExampleCount: exampleCount,
		MD5:          hex.EncodeToString(hasher.Sum(nil)),
		Source:       "raw_captures",
	}

	// Append version metadata to versions.json (JSON array on disk).
	if err := appendVersionMetadata(versionsDir, version); err != nil {
		return version, err
	}

	return version, nil
}

// PruneOldVersions deletes older snapshots for domain when more than keep
// versioned files exist. keep <= 0 means retain everything. Files are ordered
// by version number ascending; the oldest excess files (and matching
// versions.json entries) are removed. Returns the number of files pruned.
func PruneOldVersions(domain, versionsDir string, keep int) (int, error) {
	if keep <= 0 {
		return 0, nil
	}

	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("learning: read versions dir: %w", err)
	}

	type verFile struct {
		name string
		n    int
	}
	var files []verFile
	prefix := domain + "_v"
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		rest, ok := cutPrefix(name, prefix)
		if !ok {
			continue
		}
		numStr := rest
		if idx := indexByte(numStr, '.'); idx >= 0 {
			numStr = numStr[:idx]
		}
		n := 0
		valid := true
		for _, c := range numStr {
			if c < '0' || c > '9' {
				valid = false
				break
			}
			n = n*10 + int(c-'0')
		}
		if !valid || n <= 0 {
			continue
		}
		files = append(files, verFile{name: name, n: n})
	}

	if len(files) <= keep {
		return 0, nil
	}

	// Sort ascending by version number (oldest first) via simple insertion.
	for i := 1; i < len(files); i++ {
		j := i
		for j > 0 && files[j].n < files[j-1].n {
			files[j], files[j-1] = files[j-1], files[j]
			j--
		}
	}

	toDelete := files[:len(files)-keep]
	pruned := 0
	deletedNums := map[int]struct{}{}
	for _, f := range toDelete {
		path := filepath.Join(versionsDir, f.name)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return pruned, fmt.Errorf("learning: remove old version %s: %w", path, err)
		}
		deletedNums[f.n] = struct{}{}
		pruned++
	}

	// Drop matching entries from versions.json.
	if err := filterVersionMetadata(versionsDir, domain, deletedNums); err != nil {
		return pruned, err
	}
	return pruned, nil
}

// filterVersionMetadata rewrites versions.json without entries whose
// (domain, version) pair is in deletedNums.
func filterVersionMetadata(versionsDir, domain string, deletedNums map[int]struct{}) error {
	versionsFile := filepath.Join(versionsDir, "versions.json")
	data, err := os.ReadFile(versionsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("learning: read versions.json: %w", err)
	}
	var versions []DatasetVersion
	if len(data) > 0 {
		if err := json.Unmarshal(data, &versions); err != nil {
			return nil // corrupt — leave alone
		}
	}
	kept := versions[:0]
	for _, v := range versions {
		if v.Domain == domain {
			if _, drop := deletedNums[v.Version]; drop {
				continue
			}
		}
		kept = append(kept, v)
	}
	out, err := json.MarshalIndent(kept, "", "  ")
	if err != nil {
		return fmt.Errorf("learning: marshal versions: %w", err)
	}
	return os.WriteFile(versionsFile, out, 0o644)
}

func nextVersionNumber(domain, versionsDir string) (int, error) {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return 0, fmt.Errorf("learning: read versions dir: %w", err)
	}
	maxVer := 0
	prefix := domain + "_v"
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		// Only count files matching {domain}_v{N}.jsonl.
		rest, ok := cutPrefix(name, prefix)
		if !ok {
			continue
		}
		// rest looks like "3.jsonl"
		numStr := rest
		if idx := indexByte(numStr, '.'); idx >= 0 {
			numStr = numStr[:idx]
		}
		var n int
		for _, c := range numStr {
			if c < '0' || c > '9' {
				n = 0
				break
			}
			n = n*10 + int(c-'0')
		}
		if n > maxVer {
			maxVer = n
		}
	}
	return maxVer + 1, nil
}

func appendVersionMetadata(versionsDir string, v *DatasetVersion) error {
	versionsFile := filepath.Join(versionsDir, "versions.json")

	var versions []DatasetVersion
	data, err := os.ReadFile(versionsFile)
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &versions); err != nil {
			// If corrupt, start fresh.
			versions = nil
		}
	}
	versions = append(versions, *v)

	out, err := json.MarshalIndent(versions, "", "  ")
	if err != nil {
		return fmt.Errorf("learning: marshal versions: %w", err)
	}
	if err := os.WriteFile(versionsFile, out, 0o644); err != nil {
		return fmt.Errorf("learning: write versions.json: %w", err)
	}
	return nil
}

// cutPrefix is a local replacement for strings.CutPrefix (Go 1.20+) to
// avoid importing strings just for this.
func cutPrefix(s, prefix string) (string, bool) {
	if len(s) < len(prefix) {
		return s, false
	}
	for i := 0; i < len(prefix); i++ {
		if s[i] != prefix[i] {
			return s, false
		}
	}
	return s[len(prefix):], true
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
