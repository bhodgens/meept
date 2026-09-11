package daemon

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/pkg/models"
)

// E2E run 7 (2026-09-11, rkl3Th): tool-issued evidence must reach the
// step-job result as tool_evidence so the tactical claim-vs-evidence gate
// can verify file claims against the real filesystem. The collector
// absorbs tool.execution.complete payloads and drains per conversation.

func TestToolEvidenceCollector_AbsorbAndDrain(t *testing.T) {
	c := newToolEvidenceCollector()
	payload, err := json.Marshal(map[string]any{
		"conversation_id": "step-task-1-step-1",
		"tool_name":       "file_write",
		"success":         true,
		"evidence": []models.Evidence{
			models.NewEvidence(models.EvidenceFileExists, "/proj/hello.txt", "size=5", "file_write"),
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	c.absorb(payload)

	got := c.drain("step-task-1-step-1")
	if len(got) != 1 {
		t.Fatalf("got %d evidence entries, want 1", len(got))
	}
	if got[0]["subject"] != "/proj/hello.txt" {
		t.Errorf("subject = %v", got[0]["subject"])
	}
	if got[0]["tool"] != "file_write" {
		t.Errorf("tool = %v", got[0]["tool"])
	}
	// Drain clears: second drain is empty.
	if again := c.drain("step-task-1-step-1"); len(again) != 0 {
		t.Fatalf("drain not clearing: got %d again", len(again))
	}
}

func TestToolEvidenceCollector_RejectsUnsuccessfulAndEmpty(t *testing.T) {
	c := newToolEvidenceCollector()
	failed, _ := json.Marshal(map[string]any{
		"conversation_id": "conv-a",
		"tool_name":       "file_write",
		"success":         false,
	})
	c.absorb(failed)
	noEvidence, _ := json.Marshal(map[string]any{
		"conversation_id": "conv-a",
		"tool_name":       "web_search",
		"success":         true,
	})
	c.absorb(noEvidence)
	badPayload := json.RawMessage(`{not json`)
	c.absorb(badPayload)
	noConv, _ := json.Marshal(map[string]any{
		"tool_name": "file_write",
		"success":   true,
	})
	c.absorb(noConv)

	if got := c.drain("conv-a"); len(got) != 0 {
		t.Fatalf("failed/empty payloads must not collect evidence, got %v", got)
	}
}

func TestToolEvidenceCollector_SeperateConversations(t *testing.T) {
	c := newToolEvidenceCollector()
	for _, conv := range []string{"conv-a", "conv-b"} {
		payload, _ := json.Marshal(map[string]any{
			"conversation_id": conv,
			"tool_name":       "file_write",
			"success":         true,
			"evidence": []models.Evidence{
				models.NewEvidence(models.EvidenceFileExists, "/proj/"+conv+".txt", "size=1", "file_write"),
			},
		})
		c.absorb(payload)
	}
	a := c.drain("conv-a")
	b := c.drain("conv-b")
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("isolation broken: a=%d b=%d", len(a), len(b))
	}
	if a[0]["subject"] != "/proj/conv-a.txt" || b[0]["subject"] != "/proj/conv-b.txt" {
		t.Errorf("evidence crossed conversations: a=%v b=%v", a[0]["subject"], b[0]["subject"])
	}
}
