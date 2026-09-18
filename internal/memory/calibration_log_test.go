package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCalibrationLogger_PromoteReject pins the verdict-linkage contract:
// PromoteClaim / RejectClaim on auto claims append calibration records with
// the claim's extraction-time confidence.
func TestCalibrationLogger_PromoteReject(t *testing.T) {
	dir := t.TempDir()
	m := newTestManager(t)
	cal := NewCalibrationLogger(dir, nil)
	m.SetCalibrationLogger(cal)

	ctx := context.Background()
	id, err := m.StoreClaim(ctx, Claim{
		Text:       "deployments need a canary step",
		Confidence: 0.9,
		Source:     "user",
		Status:     ClaimStatusAuto,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	if err := m.PromoteClaim(ctx, id); err != nil {
		t.Fatalf("promote: %v", err)
	}
	assertCalibrationLine(t, dir, "promote", "deployments need a canary step", 0.9)

	id2, err := m.StoreClaim(ctx, Claim{
		Text:       "staging mirrors production",
		Confidence: 0.4,
		Status:     ClaimStatusAuto,
	})
	if err != nil {
		t.Fatalf("store2: %v", err)
	}
	if err := m.RejectClaim(ctx, id2); err != nil {
		t.Fatalf("reject: %v", err)
	}
	assertCalibrationLine(t, dir, "reject", "staging mirrors production", 0.4)
}

// TestCalibrationLogger_NilDisabled: no logger wired -> promote/reject still
// work (calibration never breaks the transition path).
func TestCalibrationLogger_NilDisabled(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	id, err := m.StoreClaim(ctx, Claim{Text: "x", Confidence: 0.5, Status: ClaimStatusAuto})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := m.PromoteClaim(ctx, id); err != nil {
		t.Fatalf("promote without calibration logger: %v", err)
	}
}

// TestConfFloat covers the metadata confidence extraction shapes.
func TestConfFloat(t *testing.T) {
	cases := []struct {
		in   any
		want float64
	}{{float64(0.9), 0.9}, {1, 1.0}, {json.Number("0.7"), 0.7}, {"bad", -1}, {nil, -1}}
	for _, c := range cases {
		if got := confFloat(c.in); got != c.want {
			t.Errorf("confFloat(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func assertCalibrationLine(t *testing.T, dir, verdict, text string, conf float64) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "claim_verdicts.jsonl"))
	if err != nil {
		t.Fatalf("read calibration log: %v", err)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec calibrationRecord
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec.Verdict == verdict && rec.Text == text && rec.Confidence == conf {
			found = true
		}
	}
	if !found {
		t.Fatalf("no calibration line verdict=%s text=%q conf=%v in:\n%s", verdict, text, conf, data)
	}
}
