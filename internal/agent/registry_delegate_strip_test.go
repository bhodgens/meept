package agent

import (
	"testing"
)

// DSH-7: CanDelegate=false strips the FULL delegation tool family, not just
// delegate_task — subagent_send_message / subagent_interrupt /
// subagent_list_children leak to can_delegate:false agents when
// additional_tools granted one.

// TestFilterTools_CanDelegateFalseStripsWholeFamily pins the leak: a
// non-delegating agent that was granted a control tool must not offer ANY
// member of the delegation family through the filtered registry.
func TestFilterTools_CanDelegateFalseStripsWholeFamily(t *testing.T) {
	for _, leaked := range delegationToolNames() {
		t.Run(leaked, func(t *testing.T) {
			spec := coderLikeSpec()
			spec.CanDelegate = false
			spec.AdditionalTools = append(spec.AdditionalTools, leaked)
			reg := &AgentRegistry{tools: registryWithGrants(spec)}

			filtered := reg.filterTools(spec)
			for _, family := range delegationToolNames() {
				if filtered.Get(family) != nil {
					t.Fatalf("can_delegate:false agent offered %q (grant was %q)", family, leaked)
				}
			}
			// The non-delegation grants survive the strip.
			if filtered.Get("file_read") == nil {
				t.Fatal("strip removed non-delegation grant file_read")
			}
		})
	}
}

// TestFilterTools_CanDelegateTrueKeepsControlTools: the strip is gated on
// CanDelegate=false — a delegating agent granted the control tools keeps
// them.
func TestFilterTools_CanDelegateTrueKeepsControlTools(t *testing.T) {
	spec := coderLikeSpec()
	spec.CanDelegate = true
	spec.AdditionalTools = append(spec.AdditionalTools, "subagent_list_children")
	reg := &AgentRegistry{tools: registryWithGrants(spec)}

	filtered := reg.filterTools(spec)
	if filtered.Get("subagent_list_children") == nil {
		t.Fatal("can_delegate:true agent lost a granted control tool")
	}
	if filtered.Get("delegate_task") == nil {
		t.Fatal("can_delegate:true agent lost delegate_task")
	}
}

// The unfiltered-registry escape hatch (empty allow-list -> r.tools) is
// gated on allowedTools being empty AFTER the strip. A can_delegate:false
// agent whose ONLY grant was a delegation tool must not fall through to the
// unfiltered registry (which would hand back the whole family).
func TestFilterTools_DelegationOnlyGrantDoesNotFallThrough(t *testing.T) {
	spec := &AgentSpec{
		ID:              "stripped",
		Name:            "stripped",
		Role:            RoleExecutor,
		Enabled:         true,
		CanDelegate:     false,
		AdditionalTools: []string{"subagent_send_message"},
	}
	reg := &AgentRegistry{tools: registryWithGrants(spec)}

	filtered := reg.filterTools(spec)
	if filtered == reg.tools {
		t.Fatal("delegation-only grant stripped to empty and fell through to the UNFILTERED registry")
	}
	if filtered.Get("delegate_task") != nil || filtered.Get("subagent_send_message") != nil {
		t.Fatal("unfiltered registry leaked the delegation family to a can_delegate:false agent")
	}
}
