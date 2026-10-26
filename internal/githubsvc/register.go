package githubsvc

import (
	"fmt"
)

// RegistrationOptions is the configuration surface for registering the
// GitHub PR/CI tool set with the tools Registry. It is the seam that leaf
// 05 (the builtin tool package) consumes: leaf 01 only fixes the type so
// the daemon and builtin layers agree on the fields before the real
// registration body lands.
//
// The RegisterGitHubTools(r *tools.Registry, opts RegistrationOptions)
// entry point is deliberately NOT defined in this package: the body lives
// in the builtin package (leaf 05) to avoid an import cycle (builtin
// already imports githubsvc for these types). A doc comment here records
// the contract; leaf 05 implements it.
type RegistrationOptions struct {
	// Pool is the per-token GitHub client pool the registered tools call.
	Pool *ClientPool
	// DefaultOwner is the owner fallback when a tool invocation omits one.
	DefaultOwner string
	// DefaultRepo is the repo fallback when a tool invocation omits one.
	DefaultRepo string
	// WorkingDir is the session working directory the tools operate in.
	WorkingDir string
}

// OwnerRepo holds a default owner/repo target and resolves per-call
// arguments against it.
type OwnerRepo struct {
	Owner string
	Repo  string
}

// Resolve returns the owner and repo to target. An explicit argument takes
// precedence; an empty argument falls back to the default held on this
// OwnerRepo. A position whose argument and default are both empty is an
// error, so the caller never targets a blank owner or repo.
func (o OwnerRepo) Resolve(ownerArg, repoArg string) (string, string, error) {
	owner := ownerArg
	if owner == "" {
		owner = o.Owner
	}
	repo := repoArg
	if repo == "" {
		repo = o.Repo
	}
	if owner == "" {
		return "", "", fmt.Errorf("githubsvc: no owner to resolve (argument and default both empty)")
	}
	if repo == "" {
		return "", "", fmt.Errorf("githubsvc: no repo to resolve (argument and default both empty)")
	}
	return owner, repo, nil
}
