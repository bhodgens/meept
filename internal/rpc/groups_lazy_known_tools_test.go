package rpc

// STALE-SNAPSHOT FIX pin (daemon wiring audit, fix 2): the daemon wires
// groups RPC tool validation at handler-registration time, BEFORE
// Components.Start registers the scheduler/ledger/platform/
// employee-messaging/voice tools. An eager SetToolValidation snapshot is
// therefore permanently stale: a VALID voice_call assignment would be
// rejected forever. The lazy SetKnownToolsProvider seam resolves
// CanonicalNames from the LIVE registry per request, so a tool registered
// only during Start validates on the RPC leg.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
)

func groupsAssignPolicyParams(policy *groups.Policy) json.RawMessage {
	raw, err := json.Marshal(map[string]any{
		// The C5 write gate admits a kernel-verified daemon-descendant
		// agent caller only through its agent_id field (self-declared id
		// checked against admin_agents).
		"agent_id": "agent-update",
		"subject":  "agent:tester",
		"policy":   policy,
	})
	if err != nil {
		panic(err)
	}
	return raw
}

// TestGroupsHandlerLazyKnownToolsProvider pins the refresh path: with a
// provider wired, a tool registered AFTER SetToolValidation (simulating
// Start-time registration) is accepted on groups.assign, and a genuinely
// unregistered tool is still rejected naming the offender.
func TestGroupsHandlerLazyKnownToolsProvider(t *testing.T) {
	cfg := &config.Config{
		Security: config.SecurityConfig{
			Groups: config.SecurityGroupsConfig{
				AdminAgents: []string{"agent-update"},
				// Empty policy table: the request's policy object is
				// upserted, so only the C6 leg must reject/accept.
				Policies: []config.GroupsPolicyConfig{},
				Bindings: []config.GroupsBindingConfig{},
			},
		},
	}
	h := NewGroupsHandler(cfg, "", nil, nil, nil)
	// Wire the daemon ordering: eager snapshot WITHOUT the later tool...
	h.SetToolValidation(map[string]struct{}{"file_read": {}}, nil)
	// ...then the lazy provider backed by a registry view that GROWS
	// (start-time registration of voice_call happens after wiring).
	known := map[string]struct{}{"file_read": {}}
	h.SetKnownToolsProvider(func() map[string]struct{} { return known })

	peerCtx := ContextWithPeerIdentity(context.Background(), PeerIdentity{PID: 1, HasPID: true, SpawnedByDaemon: true})
	req := assignRequest{AgentID: "agent-update"}

	if err := h.resolveCaller(peerCtx, &req); err != nil {
		t.Fatalf("write gate should admit a daemon-descendant admin agent: %v", err)
	}

	// (1) Before "Start" registers voice_call: rejected naming it.
	voicePolicy := &groups.Policy{Name: "voiceops", ToolsAllowed: []string{"voice_call"}}
	if _, err := h.handleAssign(peerCtx, groupsAssignPolicyParams(voicePolicy)); err == nil {
		t.Fatal("voice_call accepted before start-time registration; want C6 rejection")
	} else if !strings.Contains(err.Error(), "voice_call") {
		t.Fatalf("C6 rejection does not name the offender: %v", err)
	}

	// (2) Start registers voice_call into the live registry view.
	known["voice_call"] = struct{}{}

	if _, err := h.handleAssign(peerCtx, groupsAssignPolicyParams(voicePolicy)); err != nil {
		t.Fatalf("valid post-Start-registered tool rejected on the RPC leg (stale snapshot): %v", err)
	}

	// (3) The gate still bites: a genuinely unregistered name is rejected
	// against the same live set.
	badPolicy := &groups.Policy{Name: "typos", ToolsAllowed: []string{"definitely_not_a_registered_tool"}}
	if _, err := h.handleAssign(peerCtx, groupsAssignPolicyParams(badPolicy)); err == nil {
		t.Fatal("unregistered tool accepted on the RPC leg; want C6 rejection")
	} else if !strings.Contains(err.Error(), "definitely_not_a_registered_tool") {
		t.Fatalf("rejection does not name the offender: %v", err)
	}
}

// TestGroupsHandlerLiveKnownToolsFallbacks pins the seam's degradation:
// a nil provider keeps the snapshot; a provider returning nil (racing
// shutdown) falls back to the snapshot instead of widening the gate to
// "no validation".
func TestGroupsHandlerLiveKnownToolsFallbacks(t *testing.T) {
	snapshot := map[string]struct{}{"file_read": {}}

	h := NewGroupsHandler(nil, "", nil, nil, nil)
	h.SetToolValidation(snapshot, nil)
	if got := h.liveKnownTools(); len(got) != 1 {
		t.Fatalf("nil provider: want snapshot, got %v", got)
	}

	h.SetKnownToolsProvider(func() map[string]struct{} { return nil })
	if got := h.liveKnownTools(); len(got) != 1 {
		t.Fatalf("provider returning nil: want snapshot fallback (never widen the gate), got %v", got)
	}
}
