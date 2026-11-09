package runtime

import (
	"errors"
	"fmt"
	"strings"
)

// Network class names. The groups policy field uses unrestricted / allowlist /
// none. Issue 71 also names domain-allowlist; it is the same rank as allowlist.
const (
	NetworkClassUnrestricted    = "unrestricted"
	NetworkClassAllowlist       = "allowlist"
	NetworkClassDomainAllowlist = "domain-allowlist"
	NetworkClassNone            = "none"
)

// ErrNetworkClassDenied is returned when a container backend cannot honor a
// restrictive class (its network mode is not already "none").
var ErrNetworkClassDenied = errors.New("network class denies egress")

// ErrNetworkClassUnenforceable is returned when the host cannot confine a
// restrictive class. Callers must not launch the command: an unconfined
// launch would be unrestricted egress.
var ErrNetworkClassUnenforceable = errors.New("network class cannot be enforced")

// NetworkClassRestricts reports whether class requires egress confinement
// at launch. Empty and "unrestricted" are the legacy unmanaged posture
// (no change). none, allowlist, domain-allowlist, and any unknown class
// restrict: an unrecognized class must not widen access.
//
// allowlist and domain-allowlist fail closed to no egress on the local
// backend. The groups field is a class, not a host list, and EgressPolicy
// is not armed on a path that can see raw sockets. Sandboxing all egress
// is the enforcement that does not invent a packet filter. A later proxy
// lane can narrow this without widening the default.
func NetworkClassRestricts(class string) bool {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "", NetworkClassUnrestricted:
		return false
	default:
		return true
	}
}

// RequireContainerNetwork refuses a container launch whose network mode
// would ignore a restrictive class. mode "none" already has no egress.
// Empty mode is the docker default (bridge) and is treated as open.
func RequireContainerNetwork(mode, class string) error {
	if !NetworkClassRestricts(class) {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(mode), "none") {
		return nil
	}
	got := strings.TrimSpace(mode)
	if got == "" {
		got = "default"
	}
	return fmt.Errorf("%w: class %q requires container network_mode=none (have %q)", ErrNetworkClassDenied, class, got)
}
