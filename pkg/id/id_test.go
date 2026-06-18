package id

import (
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// hexSuffixRe matches exactly 16 lowercase hex chars at end of string.
var hexSuffixRe = regexp.MustCompile(`[0-9a-f]{16}$`)

// TestGenerate_Format verifies the ID shape produced by Generate.
//
// Contract documented in id.go:
//   - 8 random bytes encoded as hex → exactly 16 hex chars
//   - prefix is prepended verbatim
//   - hex portion is lowercase (encoding/hex default)
func TestGenerate_Format(t *testing.T) {
	cases := []string{"", "x", "job-", "session-", "CamelCase-", "with_underscore-"}
	for _, prefix := range cases {
		t.Run("prefix="+prefix, func(t *testing.T) {
			got := Generate(prefix)
			if !strings.HasPrefix(got, prefix) {
				t.Fatalf("Generate(%q) = %q; must start with prefix", prefix, got)
			}
			suffix := strings.TrimPrefix(got, prefix)
			if len(suffix) != 16 {
				t.Fatalf("Generate(%q) suffix %q is %d chars; expected 16", prefix, suffix, len(suffix))
			}
			if !hexSuffixRe.MatchString(suffix) {
				t.Fatalf("Generate(%q) suffix %q is not 16 lowercase hex chars", prefix, suffix)
			}
		})
	}
}

// TestGenerate_SuffixLength verifies the random portion is exactly 16 hex chars
// (8 bytes). Longer would waste bytes; shorter would raise collision probability.
func TestGenerate_SuffixLength(t *testing.T) {
	for i := 0; i < 1000; i++ {
		got := Generate("t-")
		// Strip prefix "t-"
		suffix := strings.TrimPrefix(got, "t-")
		if len(suffix) != 16 {
			t.Fatalf("iteration %d: suffix %q is %d chars; expected 16", i, suffix, len(suffix))
		}
	}
}

// TestGenerate_Uniqueness generates many IDs concurrently and asserts no collisions.
//
// With 8 bytes of entropy (64 bits), the birthday-paradox collision threshold is
// around 2^32 ≈ 4 billion IDs. We generate far fewer (1M) and assert uniqueness
// to catch catastrophic bugs (e.g., zero entropy, modulo bias causing clustering).
func TestGenerate_Uniqueness(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping uniqueness test in short mode")
	}
	const n = 100_000
	const workers = 16
	var wg sync.WaitGroup
	results := make([][]string, workers)
	for w := range workers {
		results[w] = make([]string, n/workers)
	}

	wg.Add(workers)
	for w := range workers {
		go func(workerIdx int) {
			defer wg.Done()
			for i := range results[workerIdx] {
				results[workerIdx][i] = Generate("uniq-")
			}
		}(w)
	}
	wg.Wait()

	seen := make(map[string]struct{}, n)
	for _, batch := range results {
		for _, id := range batch {
			if _, dup := seen[id]; dup {
				t.Fatalf("duplicate ID generated: %s", id)
			}
			seen[id] = struct{}{}
		}
	}
	if len(seen) != n {
		t.Fatalf("expected %d unique IDs, got %d", n, len(seen))
	}
}

// TestGenerate_ConcurrentSafety hammers Generate from many goroutines to verify
// crypto/rand is goroutine-safe and the function does not panic or race.
//
// Run with: go test -race ./pkg/id/...
func TestGenerate_ConcurrentSafety(t *testing.T) {
	const goroutines = 32
	const iterations = 1000
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range iterations {
				_ = Generate("race-")
			}
		}()
	}
	wg.Wait()
}

// TestGenerate_HexLowercase verifies the hex portion is lowercase, matching
// encoding/hex.EncodeToString's documented behavior. Mixed case would break
// callers that do byte-level comparison or sort IDs lexicographically.
func TestGenerate_HexLowercase(t *testing.T) {
	for i := 0; i < 1000; i++ {
		got := Generate("lc-")
		suffix := strings.TrimPrefix(got, "lc-")
		for _, r := range suffix {
			if r >= 'A' && r <= 'F' {
				t.Fatalf("ID %q contains uppercase hex char %q; expected lowercase", got, r)
			}
		}
	}
}

// TestGenerate_DistributionFairness verifies that each hex character appears
// with reasonable frequency. Catastrophic modulo bias or a broken RNG would
// skew the distribution heavily toward a few characters.
//
// Note: crypto/rand produces uniformly distributed bytes, so this is a
// sanity check, not a rigorous statistical test.
func TestGenerate_DistributionFairness(t *testing.T) {
	const sampleSize = 10_000
	counts := make(map[byte]int, 16)
	for i := 0; i < sampleSize; i++ {
		got := Generate("dist-")
		suffix := strings.TrimPrefix(got, "dist-")
		for _, c := range []byte(suffix) {
			counts[c]++
		}
	}
	// Each of the 16 hex chars should appear roughly sampleSize*16/16 = sampleSize
	// times. We allow [0.5x, 2x] of the expected count, which is wildly lenient
	// for true uniform randomness — designed to catch only catastrophic bugs.
	expected := sampleSize // total bytes / 16
	for _, hex := range "0123456789abcdef" {
		got := counts[byte(hex)]
		// Allow down to 40% and up to 200% of expected (very lenient to avoid flakes)
		low, high := uint64(expected*4/10), uint64(expected*2)
		if uint64(got) < low {
			t.Errorf("hex char %q underrepresented: %d (expected >= %d)", string(hex), got, low)
		}
		if uint64(got) > high {
			t.Errorf("hex char %q overrepresented: %d (expected <= %d)", string(hex), got, high)
		}
	}
}

// TestGenerate_NilSafePrefix verifies Generate does not panic on empty prefix
// and produces a valid bare-hex ID.
func TestGenerate_NilSafePrefix(t *testing.T) {
	got := Generate("")
	if len(got) != 16 {
		t.Fatalf("Generate(\"\") = %q (len %d); expected exactly 16 hex chars", got, len(got))
	}
}

// TestGenerate_NoPredictableSequence verifies that sequential calls do not
// produce sequential IDs (which would defeat the purpose of pkg/id over
// time.Now().UnixNano()). Two consecutive IDs should not share more than
// half their hex chars.
func TestGenerate_NoPredictableSequence(t *testing.T) {
	const samples = 1000
	for i := 0; i < samples; i++ {
		a := Generate("seq-")
		b := Generate("seq-")
		sa := strings.TrimPrefix(a, "seq-")
		sb := strings.TrimPrefix(b, "seq-")
		same := 0
		for j := 0; j < len(sa) && j < len(sb); j++ {
			if sa[j] == sb[j] {
				same++
			}
		}
		// 16 hex chars; expect ~1 match by chance. >12 identical would be
		// suspicious (suggesting the two IDs are nearly identical).
		if same > 12 {
			t.Fatalf("sequential IDs share %d/%d chars: %q vs %q", same, len(sa), a, b)
		}
	}
}

// TestGenerate_BenchmarkSmoke runs Generate in a tight loop to ensure it
// is fast enough for production use. crypto/rand.Read on Linux/macOS/Windows
// should sustain >100k ops/sec; we assert >1k ops/sec to avoid flakes on CI.
//
// This is a smoke test, not a proper benchmark. For real numbers:
//
//	go test -bench=. ./pkg/id/...
func TestGenerate_BenchmarkSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark smoke test in short mode")
	}
	const n = 10_000
	start := time.Now()
	for range n {
		_ = Generate("bench-")
	}
	elapsedPerOp := time.Since(start).Nanoseconds() / int64(n)
	// >1ms/op would indicate a serious perf regression (crypto/rand syscalls
	// are typically ~1-10μs on modern hardware). 1ms threshold is very lenient.
	if elapsedPerOp > 1_000_000 {
		t.Fatalf("Generate avg %d ns/op (>%d); too slow", elapsedPerOp, 1_000_000)
	}
}

// BenchmarkGenerate is the canonical benchmark. Run with:
//
//	go test -bench=. ./pkg/id/...
func BenchmarkGenerate(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_ = Generate("bench-")
	}
}

// BenchmarkGenerate_Parallel exercises concurrent crypto/rand access.
func BenchmarkGenerate_Parallel(b *testing.B) {
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = Generate("bench-")
		}
	})
}
