package acp

import "testing"

// TestApproverSlackKindString pins hitl-notifications leaf 01 Contract C4:
// Approver{Kind: "slack", Detail: "<uid>"} renders "slack:<uid>".
func TestApproverSlackKindString(t *testing.T) {
	a := Approver{Kind: ApproverKindSlack, Detail: "U123ABC"}
	if got := a.String(); got != "slack:U123ABC" {
		t.Fatalf("String() = %q, want %q", got, "slack:U123ABC")
	}
	// A slack approver without a Detail degrades to the bare kind, never
	// "slack:" with a dangling colon.
	if got := (Approver{Kind: ApproverKindSlack}).String(); got != "slack" {
		t.Fatalf("empty-detail String() = %q, want %q", got, "slack")
	}
	// The kind constant is the pinned plain string (internal/comm/slack
	// holds no import back into acp; the value is the contract).
	if ApproverKindSlack != "slack" {
		t.Fatalf("ApproverKindSlack = %q, want %q", ApproverKindSlack, "slack")
	}
}
