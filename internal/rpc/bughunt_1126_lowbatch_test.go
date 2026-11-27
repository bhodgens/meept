package rpc

import (
	"testing"

	"github.com/caimlas/meept/internal/groups"
)

// TestNilReceiverSetters pins the repo rule "nil-guard every Set*": calling
// each of the four setters on a nil receiver must be a no-op, never a panic
// (bughunt A1-16). Before the fix, SetToolValidation, SetDiagnosisService,
// SetOverridesLoader and SetGroupsHandler all dereferenced their receiver
// without a nil guard.
func TestNilReceiverSetters(t *testing.T) {
	var h *GroupsHandler
	h.SetToolValidation(map[string]struct{}{"shell": {}}, func(string) string { return "" })
	h.SetDiagnosisService(nil)
	h.SetOverridesLoader(func() (groups.OverridesReader, error) { return nil, nil })

	var p *ProxyHandler
	p.SetGroupsHandler(nil)
}
