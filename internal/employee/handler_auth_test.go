package employee

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/rpc"
)

func ownerPeer() rpc.PeerIdentity {
	return rpc.PeerIdentity{
		PID:    os.Getpid(),
		UID:    os.Getuid(),
		HasPID: true,
		HasUID: true,
	}
}

func withPeer(peer rpc.PeerIdentity) context.Context {
	return rpc.ContextWithPeerIdentity(context.Background(), peer)
}

func TestEffectiveApprover(t *testing.T) {
	t.Parallel()

	owner := ownerPeer()
	spawned := owner
	spawned.SpawnedByDaemon = true
	otherUID := owner
	otherUID.UID = os.Getuid() + 1
	noUID := owner
	noUID.HasUID = false

	injected := &CallerInfo{UserID: "auditor"}
	empty := &CallerInfo{}

	tests := []struct {
		name    string
		ctx     context.Context
		wantID  string
		wantNil bool
		sameAs  *CallerInfo
	}{
		{name: "no caller no peer", ctx: context.Background(), wantNil: true},
		{name: "nil ctx", ctx: nil, wantNil: true},
		{name: "owner peer", ctx: withPeer(owner), wantID: "user"},
		{name: "spawned by daemon", ctx: withPeer(spawned), wantNil: true},
		{name: "other uid", ctx: withPeer(otherUID), wantNil: true},
		{name: "uid unverified", ctx: withPeer(noUID), wantNil: true},
		{
			name:   "injected caller wins over owner peer",
			ctx:    ContextWithCaller(withPeer(owner), injected),
			wantID: "auditor",
			sameAs: injected,
		},
		{
			name:   "empty injected caller is not upgraded to user",
			ctx:    ContextWithCaller(withPeer(owner), empty),
			wantID: "",
			sameAs: empty,
		},
		{
			name:   "injected caller without peer",
			ctx:    ContextWithCaller(context.Background(), injected),
			wantID: "auditor",
			sameAs: injected,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := effectiveApprover(tt.ctx)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("got %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("got nil")
			}
			if got.UserID != tt.wantID {
				t.Fatalf("UserID = %q, want %q", got.UserID, tt.wantID)
			}
			if tt.sameAs != nil && got != tt.sameAs {
				t.Fatal("injected caller was replaced")
			}
		})
	}
}

type recordingPlanDisposer struct {
	approveBy string
	rejectBy  string
}

func (d *recordingPlanDisposer) ApprovePlan(_ context.Context, _, _, by string) error {
	d.approveBy = by
	return nil
}

func (d *recordingPlanDisposer) RejectPlan(_ context.Context, _, _, by, _ string) error {
	d.rejectBy = by
	return nil
}

func approveParams(t *testing.T, goalID, planID, reason string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(goalApproveRequest{GoalID: goalID, PlanID: planID, Reason: reason})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func rejectParams(t *testing.T, goalID, planID, reason string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(goalRejectRequest{GoalID: goalID, PlanID: planID, Reason: reason})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func TestHandleGoalsApproveAuth(t *testing.T) {
	t.Parallel()

	t.Run("no caller no peer", func(t *testing.T) {
		t.Parallel()
		h := NewRPCHandler(NewManager(nil))
		_, err := h.handleGoalsApprove(context.Background(), approveParams(t, "g", "p", ""))
		if err == nil || !strings.Contains(err.Error(), "unauthenticated: API key required for plan approval") {
			t.Fatalf("err = %v, want unauthenticated plan approval", err)
		}
	})

	t.Run("spawned by daemon", func(t *testing.T) {
		t.Parallel()
		spawned := ownerPeer()
		spawned.SpawnedByDaemon = true
		h := NewRPCHandler(NewManager(nil))
		_, err := h.handleGoalsApprove(withPeer(spawned), approveParams(t, "g", "p", ""))
		if err == nil || !strings.Contains(err.Error(), "unauthenticated: API key required for plan approval") {
			t.Fatalf("err = %v, want unauthenticated plan approval", err)
		}
	})

	t.Run("owner peer approves as user", func(t *testing.T) {
		t.Parallel()
		m, empID := newGoalTestManager(t)
		goal, err := m.CreateGoal(context.Background(), GoalSpec{
			EmployeeID: empID,
			Title:      "approve via socket",
			Mandate:    "owner unix peer may approve",
			Source:     "user",
		})
		if err != nil {
			t.Fatalf("CreateGoal: %v", err)
		}
		disposer := &recordingPlanDisposer{}
		m.SetPlanDisposer(disposer)
		h := NewRPCHandler(m)

		result, err := h.handleGoalsApprove(withPeer(ownerPeer()), approveParams(t, goal.ID, "plan-1", "ok"))
		if err != nil {
			t.Fatalf("handleGoalsApprove: %v", err)
		}
		resp, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("result type = %T", result)
		}
		if resp["approver"] != "user" || resp["status"] != "approved" {
			t.Fatalf("result = %#v, want approver user status approved", resp)
		}
		if disposer.approveBy != "user" {
			t.Fatalf("disposer by = %q, want user", disposer.approveBy)
		}
	})

	t.Run("injected caller wins over owner peer", func(t *testing.T) {
		t.Parallel()
		m, empID := newGoalTestManager(t)
		goal, err := m.CreateGoal(context.Background(), GoalSpec{
			EmployeeID: empID,
			Title:      "caller wins",
			Mandate:    "injected identity is not replaced by the peer",
			Source:     "user",
		})
		if err != nil {
			t.Fatalf("CreateGoal: %v", err)
		}
		m.SetPlanDisposer(&recordingPlanDisposer{})
		h := NewRPCHandler(m)
		ctx := ContextWithCaller(withPeer(ownerPeer()), &CallerInfo{UserID: "other"})

		_, err = h.handleGoalsApprove(ctx, approveParams(t, goal.ID, "plan-1", "ok"))
		if err == nil || !strings.Contains(err.Error(), `caller "other"`) {
			t.Fatalf("err = %v, want unauthorized for caller \"other\"", err)
		}
	})
}

func TestHandleGoalsRejectAuth(t *testing.T) {
	t.Parallel()

	t.Run("no caller no peer", func(t *testing.T) {
		t.Parallel()
		h := NewRPCHandler(NewManager(nil))
		_, err := h.handleGoalsReject(context.Background(), rejectParams(t, "g", "p", "no"))
		if err == nil || !strings.Contains(err.Error(), "unauthenticated: API key required for plan rejection") {
			t.Fatalf("err = %v, want unauthenticated plan rejection", err)
		}
	})

	t.Run("spawned by daemon", func(t *testing.T) {
		t.Parallel()
		spawned := ownerPeer()
		spawned.SpawnedByDaemon = true
		h := NewRPCHandler(NewManager(nil))
		_, err := h.handleGoalsReject(withPeer(spawned), rejectParams(t, "g", "p", "no"))
		if err == nil || !strings.Contains(err.Error(), "unauthenticated: API key required for plan rejection") {
			t.Fatalf("err = %v, want unauthenticated plan rejection", err)
		}
	})

	t.Run("owner peer rejects as user", func(t *testing.T) {
		t.Parallel()
		m, empID := newGoalTestManager(t)
		goal, err := m.CreateGoal(context.Background(), GoalSpec{
			EmployeeID: empID,
			Title:      "reject via socket",
			Mandate:    "owner unix peer may reject",
			Source:     "user",
		})
		if err != nil {
			t.Fatalf("CreateGoal: %v", err)
		}
		disposer := &recordingPlanDisposer{}
		m.SetPlanDisposer(disposer)
		h := NewRPCHandler(m)

		result, err := h.handleGoalsReject(withPeer(ownerPeer()), rejectParams(t, goal.ID, "plan-1", "not now"))
		if err != nil {
			t.Fatalf("handleGoalsReject: %v", err)
		}
		resp, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("result type = %T", result)
		}
		if resp["rejector"] != "user" || resp["status"] != "rejected" {
			t.Fatalf("result = %#v, want rejector user status rejected", resp)
		}
		if disposer.rejectBy != "user" {
			t.Fatalf("disposer by = %q, want user", disposer.rejectBy)
		}
	})
}

func TestHandleGoalsApproveGateAuth(t *testing.T) {
	t.Parallel()

	gateParams := func(t *testing.T, goalID string) json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(map[string]string{"goal_id": goalID})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return raw
	}

	t.Run("no caller no peer", func(t *testing.T) {
		t.Parallel()
		h := NewRPCHandler(NewManager(nil))
		_, err := h.handleGoalsApproveGate(context.Background(), gateParams(t, "g"))
		if err == nil || !strings.Contains(err.Error(), "unauthenticated: API key required for gate approval") {
			t.Fatalf("err = %v, want unauthenticated gate approval", err)
		}
	})

	t.Run("spawned by daemon", func(t *testing.T) {
		t.Parallel()
		spawned := ownerPeer()
		spawned.SpawnedByDaemon = true
		h := NewRPCHandler(NewManager(nil))
		_, err := h.handleGoalsApproveGate(withPeer(spawned), gateParams(t, "g"))
		if err == nil || !strings.Contains(err.Error(), "unauthenticated: API key required for gate approval") {
			t.Fatalf("err = %v, want unauthenticated gate approval", err)
		}
	})

	t.Run("owner peer approves gate as user", func(t *testing.T) {
		t.Parallel()
		m, empID := newGoalTestManager(t)
		goal, err := m.CreateGoal(context.Background(), GoalSpec{
			EmployeeID: empID,
			Title:      "gate via socket",
			Mandate:    "owner unix peer may approve a gate",
			Source:     "user",
		})
		if err != nil {
			t.Fatalf("CreateGoal: %v", err)
		}
		m.SetGoalGateResumeFn(func(context.Context, string) bool { return true })
		h := NewRPCHandler(m)

		result, err := h.handleGoalsApproveGate(withPeer(ownerPeer()), gateParams(t, goal.ID))
		if err != nil {
			t.Fatalf("handleGoalsApproveGate: %v", err)
		}
		resp, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("result type = %T", result)
		}
		if resp["approver"] != "user" || resp["resumed"] != true {
			t.Fatalf("result = %#v, want approver user resumed true", resp)
		}
	})
}
