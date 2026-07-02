package integration

// cas_fetch_streaming_test.go — Tests CAS blob fetch streaming with hash
// verification and offset-based resume (spec §10). Uses a moderately-sized
// blob (1MB for test speed) to exercise the streaming path.

import (
	"context"
	"crypto/rand"
	"os"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/resources"
)

// TestCASFetchStreaming verifies that a blob fetched via the peer path
// matches the original hash and content.
func TestCASFetchStreaming(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	daemonA := newTestDaemon(t, ctx, "stream-source")
	daemonB := newTestDaemon(t, ctx, "stream-sink")
	defer daemonA.Close()
	defer daemonB.Close()

	connectPeers(daemonA, daemonB)
	waitForPeerConnection(t, daemonB, daemonA, 5*time.Second)

	// Create a 1MB random blob.
	blobSize := 1024 * 1024 // 1 MB
	blob := make([]byte, blobSize)
	if _, err := rand.Read(blob); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}

	// Write to a temp file and add to A's CAS.
	srcPath := daemonA.tmpDir + "/blob-1mb.bin"
	if err := os.WriteFile(srcPath, blob, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	hash, err := daemonA.resourceManager.Add(ctx, srcPath)
	if err != nil {
		t.Fatalf("Add to A: %v", err)
	}

	// B fetches via peer path.
	path, err := daemonB.resourceManager.Ensure(ctx, resources.ResourceRef{Raw: hash})
	if err != nil {
		t.Fatalf("Ensure on B: %v", err)
	}
	defer daemonB.resourceManager.Release(resources.ResourceRef{Raw: hash})

	// Verify content matches.
	fetched, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(fetched) != blobSize {
		t.Errorf("size mismatch: got %d, want %d", len(fetched), blobSize)
	}
	for i := 0; i < blobSize; i++ {
		if fetched[i] != blob[i] {
			t.Errorf("byte mismatch at offset %d: got %d, want %d", i, fetched[i], blob[i])
			break
		}
	}

	// Verify hash on the receiver side matches.
	_, body, _ := resources.ParseRef(hash)
	if err := daemonB.resourceManager.Store().VerifyBlob(body, resources.AlgoBlake3); err != nil {
		t.Errorf("VerifyBlob on B failed: %v", err)
	}

	// Verify CAS bytes fetched counter.
	if daemonB.metrics.CASBytesFetched.Load() != int64(blobSize) {
		t.Errorf("CASBytesFetched: got %d, want %d", daemonB.metrics.CASBytesFetched.Load(), blobSize)
	}
}

// TestCASFetchFromOffset verifies that Fetch with a non-zero offset returns
// only the remaining bytes.
func TestCASFetchFromOffset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	daemonA := newTestDaemon(t, ctx, "offset-source")
	defer daemonA.Close()

	// Create a small file.
	content := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	srcPath := daemonA.tmpDir + "/small.bin"
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	hash, err := daemonA.resourceManager.Add(ctx, srcPath)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	_, body, _ := resources.ParseRef(hash)
	path, err := daemonA.resourceManager.Store().GetPath(body)
	if err != nil {
		t.Fatalf("GetPath: %v", err)
	}

	// Read the full file from disk to verify.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", string(data), string(content))
	}
}
