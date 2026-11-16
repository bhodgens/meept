package daemon

import (
	"context"
	"errors"

	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/services"
)

// groupsEmployeeResolver adapts *employee.Manager to
// services.EmployeeExplainResolver. services must not import employee:
// services -> employee -> bot -> comm/http -> services. The daemon is
// the composition point, same as employeeServiceAdapter.
type groupsEmployeeResolver struct {
	mgr *employee.Manager
}

// newGroupsEmployeeResolver returns nil when mgr is nil so the setter
// no-ops and explain stays roster-only. A typed-nil manager must not be
// boxed into the interface.
func newGroupsEmployeeResolver(mgr *employee.Manager) services.EmployeeExplainResolver {
	if mgr == nil {
		return nil
	}
	return groupsEmployeeResolver{mgr: mgr}
}

// ExplainEmployee returns the employee row groups explain prints.
// ErrEmployeeNotFound and a nil employee are not-found, not errors, so
// a missing employee still fails the roster-miss path.
func (r groupsEmployeeResolver) ExplainEmployee(ctx context.Context, id string) (services.EmployeeExplain, bool, error) {
	if r.mgr == nil {
		return services.EmployeeExplain{}, false, nil
	}
	emp, err := r.mgr.GetEmployee(ctx, id)
	if err != nil {
		if errors.Is(err, employee.ErrEmployeeNotFound) {
			return services.EmployeeExplain{}, false, nil
		}
		return services.EmployeeExplain{}, false, err
	}
	if emp == nil {
		return services.EmployeeExplain{}, false, nil
	}
	return services.EmployeeExplain{
		ID:                  emp.ID,
		ConstitutionPresent: emp.HasConstitution(),
		ToolsAllowed:        append([]string(nil), emp.Constitution.Constraints.ToolsAllowed...),
		ToolsForbidden:      append([]string(nil), emp.Constitution.Constraints.ToolsForbidden...),
	}, true, nil
}

// Compile-time: the adapter satisfies the service resolver.
var _ services.EmployeeExplainResolver = groupsEmployeeResolver{}
