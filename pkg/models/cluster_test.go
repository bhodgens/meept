package models

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"
)

func TestClusterEvent_SignAndVerify(t *testing.T) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("key gen failed: %v", err)
	}

	event := &ClusterEvent{
		EventID:   "test-event-001",
		NodeID:    "node-01",
		EventType: EventTaskCreate,
		Timestamp: time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC),
		Payload:   json.RawMessage(`{"task_id":"t1","agent_id":"coder"}`),
	}

	err = event.Sign(privKey)
	if err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	if len(event.Signature) == 0 {
		t.Fatal("signature is empty after signing")
	}

	if !event.Verify(pubKey) {
		t.Error("verification failed for valid signature")
	}

	// Tamper with payload
	event.Payload[0] ^= 0xFF
	if event.Verify(pubKey) {
		t.Error("verification passed for tampered event")
	}
}

func TestClusterEvent_SignWithWrongKey(t *testing.T) {
	pubKey1, privKey1, _ := ed25519.GenerateKey(rand.Reader)
	pubKey2, _, _ := ed25519.GenerateKey(rand.Reader)

	event := &ClusterEvent{
		EventID:   "test-event-wrong-key",
		NodeID:    "node-01",
		EventType: EventTaskComplete,
		Timestamp: time.Now(),
		Payload:   json.RawMessage(`{}`),
	}

	if err := event.Sign(privKey1); err != nil {
		t.Fatalf("sign failed: %v", err)
	}

	if event.Verify(pubKey2) {
		t.Error("verification passed for wrong public key")
	}

	if !event.Verify(pubKey1) {
		t.Error("verification failed for correct public key")
	}
}

func TestClusterEvent_MarshalUnmarshal(t *testing.T) {
	orig := &ClusterEvent{
		EventID:   "test-001",
		NodeID:    "node-test",
		EventType: EventTaskClaim,
		Timestamp: time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC),
		VectorClock: map[string]int64{
			"node-test": 1,
		},
		Payload: json.RawMessage(`{"task_id":"t1"}`),
	}

	data, err := orig.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ClusterEvent
	if err := decoded.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.EventID != orig.EventID {
		t.Errorf("EventID mismatch: got %s, want %s", decoded.EventID, orig.EventID)
	}
	if decoded.NodeID != orig.NodeID {
		t.Errorf("NodeID mismatch: got %s, want %s", decoded.NodeID, orig.NodeID)
	}
	if decoded.EventType != orig.EventType {
		t.Errorf("EventType mismatch: got %s, want %s", decoded.EventType, orig.EventType)
	}
	if decoded.Timestamp.UnixNano() != orig.Timestamp.UnixNano() {
		t.Errorf("Timestamp mismatch: got %d, want %d", decoded.Timestamp.UnixNano(), orig.Timestamp.UnixNano())
	}
	if decoded.VectorClock["node-test"] != 1 {
		t.Errorf("VectorClock mismatch")
	}
	if string(decoded.Payload) != string(orig.Payload) {
		t.Errorf("Payload mismatch: got %s, want %s", decoded.Payload, orig.Payload)
	}
}

func TestClusterEvent_TimestampPreservedThroughMarshal(t *testing.T) {
	precise := time.Date(2026, 1, 15, 8, 30, 45, 123456789, time.UTC)
	orig := &ClusterEvent{
		EventID:   "ts-test",
		NodeID:    "node-ts",
		EventType: EventNodeHeartbeat,
		Timestamp: precise,
	}

	data, err := orig.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded ClusterEvent
	if err := decoded.UnmarshalJSON(data); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Timestamp.UnixNano() != precise.UnixNano() {
		t.Errorf("timestamp not preserved: got %v, want %v",
			decoded.Timestamp, precise)
	}
}

func TestGenerateEventID(t *testing.T) {
	id1 := GenerateEventID()
	id2 := GenerateEventID()

	if id1 == id2 {
		t.Error("two event IDs are identical")
	}

	if len(id1) == 0 {
		t.Error("event ID is empty")
	}

	// Should produce hex string from 16 bytes = 32 hex chars
	if len(id1) != 32 {
		t.Errorf("event ID length mismatch: got %d, want 32", len(id1))
	}
}

func TestClusterEventType_Constants(t *testing.T) {
	expected := []ClusterEventType{
		EventTaskCreate,
		EventTaskClaim,
		EventTaskComplete,
		EventTaskFail,
		EventTaskReclaim,
		EventTaskPause,
		EventTaskResume,
		EventNodeJoin,
		EventNodeLeave,
		EventNodeHeartbeat,
	}

	for _, et := range expected {
		if len(et) == 0 {
			t.Errorf("empty ClusterEventType constant")
		}
		// Should start with uppercase
		if et[0] < 'A' || et[0] > 'Z' {
			t.Errorf("event type %q does not start with an uppercase letter", et)
		}
	}
}

func TestPayload_Structs(t *testing.T) {
	// TaskPayload
	tasks := TaskPayload{
		TaskID:      "t-001",
		AgentID:     "coder",
		Description: "fix bug",
		Input:       map[string]any{"file": "main.go"},
		Constraints: []string{"no-git-reset"},
		Priority:    5,
		CreatedBy:   "node-01",
	}
	data, err := json.Marshal(tasks)
	if err != nil {
		t.Fatalf("TaskPayload marshal failed: %v", err)
	}
	var decoded TaskPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("TaskPayload unmarshal failed: %v", err)
	}
	if decoded.TaskID != "t-001" {
		t.Errorf("TaskPayload TaskID mismatch")
	}

	// ClaimPayload
	timeout := time.Now().Add(5 * time.Minute)
	cl := ClaimPayload{
		TaskID:    "t-001",
		ClaimedBy: "node-02",
		TimeoutAt: timeout,
	}
	data, err = json.Marshal(cl)
	if err != nil {
		t.Fatalf("ClaimPayload marshal failed: %v", err)
	}
	var decodedClaim ClaimPayload
	if err := json.Unmarshal(data, &decodedClaim); err != nil {
		t.Fatalf("ClaimPayload unmarshal failed: %v", err)
	}
	if decodedClaim.TaskID != "t-001" {
		t.Errorf("ClaimPayload TaskID mismatch")
	}

	// ReclaimPayload
	rl := ReclaimPayload{
		TaskID:      "t-001",
		Reason:      "timeout",
		ReclaimedBy: "node-03",
	}
	data, err = json.Marshal(rl)
	if err != nil {
		t.Fatalf("ReclaimPayload marshal failed: %v", err)
	}
	var decodedReclaim ReclaimPayload
	if err := json.Unmarshal(data, &decodedReclaim); err != nil {
		t.Fatalf("ReclaimPayload unmarshal failed: %v", err)
	}
	if decodedReclaim.Reason != "timeout" {
		t.Errorf("ReclaimPayload Reason mismatch")
	}

	// NodePayload
	_, signingPub, _ := ed25519.GenerateKey(rand.Reader)
	nl := NodePayload{
		NodeID:       "n-01",
		NodeName:     "workstation-1",
		WireGuardPub: "wg-pub-key",
		SigningPub:   signingPub,
		Endpoint:     "192.168.1.10:51820",
		Capabilities: []string{"code", "debug"},
		ClusterIP:    "10.0.0.1",
		JoinedAt:     time.Now(),
	}
	data, err = json.Marshal(nl)
	if err != nil {
		t.Fatalf("NodePayload marshal failed: %v", err)
	}
	var decodedNode NodePayload
	if err := json.Unmarshal(data, &decodedNode); err != nil {
		t.Fatalf("NodePayload unmarshal failed: %v", err)
	}
	if decodedNode.NodeID != "n-01" {
		t.Errorf("NodePayload NodeID mismatch")
	}
	if decodedNode.ClusterIP != "10.0.0.1" {
		t.Errorf("NodePayload ClusterIP mismatch")
	}
}
