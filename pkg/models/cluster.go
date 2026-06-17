package models

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

// ClusterEventType identifies the type of cluster event.
type ClusterEventType string

const (
	EventTaskCreate    ClusterEventType = "TASK_CREATE"
	EventTaskClaim     ClusterEventType = "TASK_CLAIM"
	EventTaskComplete  ClusterEventType = "TASK_COMPLETE"
	EventTaskFail      ClusterEventType = "TASK_FAIL"
	EventTaskReclaim   ClusterEventType = "TASK_RECLAIM"
	EventTaskPause     ClusterEventType = "TASK_PAUSE"
	EventTaskResume    ClusterEventType = "TASK_RESUME"
	EventNodeJoin      ClusterEventType = "NODE_JOIN"
	EventNodeLeave     ClusterEventType = "NODE_LEAVE"
	EventNodeHeartbeat ClusterEventType = "NODE_HEARTBEAT"
)

// ClusterEvent represents a signed, replicated cluster event.
type ClusterEvent struct {
	EventID     string           `json:"event_id"`
	NodeID      string           `json:"node_id"`
	EventType   ClusterEventType `json:"event_type"`
	Timestamp   time.Time        `json:"timestamp"`
	VectorClock map[string]int64 `json:"vector_clock"`
	Payload     json.RawMessage  `json:"payload"`
	Signature   []byte           `json:"signature"`
}

// Sign signs the event with an ed25519 private key.
func (e *ClusterEvent) Sign(privKey ed25519.PrivateKey) error {
	data := e.signingData()
	e.Signature = ed25519.Sign(privKey, data)
	return nil
}

// Verify verifies the event signature against the provided public key.
func (e *ClusterEvent) Verify(pubKey ed25519.PublicKey) bool {
	data := e.signingData()
	return ed25519.Verify(pubKey, data, e.Signature)
}

// signingData returns the canonical bytes to be signed (everything except the signature).
func (e *ClusterEvent) signingData() []byte {
	type signingStruct struct {
		EventID     string           `json:"event_id"`
		NodeID      string           `json:"node_id"`
		EventType   ClusterEventType `json:"event_type"`
		Timestamp   int64            `json:"timestamp"`
		VectorClock map[string]int64 `json:"vector_clock"`
		Payload     json.RawMessage  `json:"payload"`
	}
	data, _ := json.Marshal(signingStruct{
		EventID:     e.EventID,
		NodeID:      e.NodeID,
		EventType:   e.EventType,
		Timestamp:   e.Timestamp.UnixNano(),
		VectorClock: e.VectorClock,
		Payload:     e.Payload,
	})
	return data
}

// GenerateEventID creates a unique 32-character hex event ID from 16 random bytes.
//
// If crypto/rand fails (catastrophic system failure), a zero-filled ID is
// returned. Callers that require hard uniqueness guarantees should treat a
// zero-suffixed ID as a fatal signal. See pkg/id.Generate for the project's
// canonical predictable-ID-safe generator with documented fallback behavior.
func GenerateEventID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Unrecoverable host state; mirror pkg/id.Generate fallback contract.
		return hex.EncodeToString(b) // all-zero ID
	}
	return hex.EncodeToString(b)
}

// MarshalJSON implements custom JSON marshaling, encoding Timestamp as Unix nanoseconds.
func (e *ClusterEvent) MarshalJSON() ([]byte, error) {
	type Alias ClusterEvent
	return json.Marshal(&struct {
		Timestamp int64 `json:"timestamp"`
		*Alias
	}{
		Timestamp: e.Timestamp.UnixNano(),
		Alias:     (*Alias)(e),
	})
}

// UnmarshalJSON implements custom JSON unmarshaling, decoding Timestamp from Unix nanoseconds.
func (e *ClusterEvent) UnmarshalJSON(data []byte) error {
	type Alias ClusterEvent
	aux := &struct {
		Timestamp int64 `json:"timestamp"`
		*Alias
	}{
		Alias: (*Alias)(e),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	e.Timestamp = time.Unix(0, aux.Timestamp)
	return nil
}

// TaskPayload contains the serialized task data for TASK_CREATE events.
type TaskPayload struct {
	TaskID      string         `json:"task_id"`
	AgentID     string         `json:"agent_id"`
	Description string         `json:"description"`
	Input       map[string]any `json:"input"`
	Constraints []string       `json:"constraints"`
	Priority    int            `json:"priority"`
	CreatedBy   string         `json:"created_by"`
}

// ClaimPayload contains claim metadata for TASK_CLAIM events.
type ClaimPayload struct {
	TaskID    string    `json:"task_id"`
	ClaimedBy string    `json:"claimed_by"`
	TimeoutAt time.Time `json:"timeout_at"`
}

// ReclaimPayload contains reclaim metadata for TASK_RECLAIM events.
type ReclaimPayload struct {
	TaskID      string `json:"task_id"`
	Reason      string `json:"reason"`
	ReclaimedBy string `json:"reclaimed_by"`
}

// NodePayload contains node registration data for NODE_JOIN/NODE_LEAVE events.
type NodePayload struct {
	NodeID       string    `json:"node_id"`
	NodeName     string    `json:"node_name"`
	WireGuardPub string    `json:"wireguard_pubkey"`
	SigningPub   []byte    `json:"signing_pubkey"`
	Endpoint     string    `json:"endpoint"`
	Capabilities []string  `json:"capabilities"`
	ClusterIP    string    `json:"cluster_ip"`
	JoinedAt     time.Time `json:"joined_at"`
}
