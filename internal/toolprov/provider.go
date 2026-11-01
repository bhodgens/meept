// stub: implementation lands in 01-seam.md
//
// Package toolprov is the compiled-in tool provider seam (contract C1,
// docs/plans/20261031-tool-provider-seam/master.md). This file is a
// MINIMAL compile-only stub placed by the 00T test gate so the gate
// tests fail RED on assertions, not compilation. It contains types and
// declarations only - zero logic. Leaf 01-seam.md replaces this stub
// with the real implementation.
//
// Register, Factory, Names, RegisterAll, and the factories map are
// INTENTIONALLY NOT DECLARED in this stub: they are the seam functions
// 01-seam.md implements, and the gate tests must fail RED naming exactly
// them (undefined Register/RegisterAll/Names/Factory), never a
// compilation error on types. An unexported unused map would also trip
// the U1000 pre-commit gate, so leaf 01 introduces it together with
// Register.
package toolprov

import (
	"github.com/caimlas/meept/internal/secrets"
	"github.com/caimlas/meept/internal/tools"
)

// RiskClass is the permission ceiling a provider declares for ALL tools
// it contributes. Fail-closed: a provider that does not declare a class
// is treated as the most restrictive tier by consumers.
type RiskClass string

const (
	// RiskReadOnly marks a provider whose tools never mutate state.
	RiskReadOnly RiskClass = "read_only"
	// RiskMutating marks a provider whose tools may mutate state.
	RiskMutating RiskClass = "mutating"
	// RiskPrivileged marks a provider whose tools may spawn subprocesses
	// or cross trust boundaries.
	RiskPrivileged RiskClass = "privileged"
)

// Provider is the compiled-in provider seam (C1). Provider packages
// contribute tools through Discover(); the PermissionClass is the
// ceiling for every tool the provider contributes.
type Provider interface {
	// Name returns the provider identifier; it is the config key used
	// by [tool_providers] and must be unique across factories.
	Name() string
	// Discover returns the tools this provider contributes.
	Discover() []tools.Tool
	// PermissionClass returns the provider-wide risk ceiling.
	PermissionClass() RiskClass
	// Broker returns the secrets broker binding. Providers authenticate
	// via in-process Value only; child lanes get placeholders.
	Broker() *secrets.Broker
}

// factories was intentionally left out of the stub (see package comment);
// leaf 01-seam.md adds it together with Register.
