//go:build e2e

package agentgroups

// Leaf 03-membership/00T-tests T4 — mutation-gate e2e coverage note.
//
// The membership mutation gate e2e member ALREADY EXISTS in this suite and
// MUST NOT be duplicated:
//
//	agent-groups-04 (F4): TestAgentGroupsF4MutationGateDeniesNonPrivileged
//	in agent_groups_test.go — scratch daemon (harness.Start), live RPC
//	seam, both C5 legs:
//	  Leg 1 (file backstop): a spec-ALLOWED file_write to
//	    $MEEPT_HOME/users.json5 must still be denied for a non-admin agent
//	    when groups.default_managed=true (BlockedPaths backstop).
//	  Leg 2 (RPC gate): groups.assign from a caller that is neither the
//	    agent-update agent nor a groups-admin human key is denied at the
//	    seam; "method not found" is treated as RED (implementation absent).
//
// This file pins the coverage mapping so the leaf's Test Gate matrix can
// be validated against the manifest without grep archaeology. It is a
// compile-only guard: if the F4 member is renamed or removed, this test
// fails to compile and the coverage hole becomes loud.
//
// The privileged-side half of C5 (the agent-update agent's groups.assign
// SUCCEEDS and lands an audit event) is implementation-leaf 03 T2's
// TestGroupsMutationGate scope and intentionally not re-proven here at
// e2e cost while the RPC surface is still absent.

import "testing"

// F4Member is the existing mutation-gate e2e member this leaf's T4
// references instead of rewriting.
var _ = TestAgentGroupsF4MutationGateDeniesNonPrivileged

func TestAgentGroupsMutationGateCoveragePinned(t *testing.T) {
	t.Log("membership mutation-gate e2e coverage = agent-groups-04 " +
		"(TestAgentGroupsF4MutationGateDeniesNonPrivileged); not duplicated per leaf 00T-tests T4 note")
}
