package config

import (
	"context"
	"errors"
	"fmt"
)

// ErrPushNoCheckout is returned when PushLocalChanges is invoked on a
// ConfigSyncer whose underlying GitCheckout is nil (e.g. cluster disabled
// or sync not yet started). We avoid reusing ErrRepoUnreachable here so
// callers can distinguish "never configured" from "transient network
// failure"; both ultimately wrap ErrRepoUnreachable for operator-facing
// messages via errors.Is.
var ErrPushNoCheckout = errors.New("config sync: git checkout not initialized")

// PushLocalChanges commits any local working-tree changes in the config
// checkout and pushes them to the remote. It wraps
// GitCheckout.CommitAndPush with nil-guarding and meaningful errors so
// callers (RPC handler, CLI) get consistent diagnostics.
//
// The message argument becomes the commit message. If empty, a default
// "auto: config sync push" message is used.
func (s *ConfigSyncer) PushLocalChanges(ctx context.Context, message string) error {
	if s == nil {
		return ErrPushNoCheckout
	}

	// Snapshot checkout under lock, release before I/O (CLAUDE.md mutex scope).
	s.mu.Lock()
	checkout := s.checkout
	s.mu.Unlock()

	if checkout == nil {
		// Use errors.Join so errors.Is matches both sentinels. We include
		// ErrRepoUnreachable because operator-facing dashboards check it
		// as the canonical "git not reachable" signal, while internal
		// callers may check ErrPushNoCheckout to distinguish "never
		// configured" from "transient network failure".
		return errors.Join(ErrRepoUnreachable, ErrPushNoCheckout)
	}

	if message == "" {
		message = "auto: config sync push"
	}

	if err := checkout.CommitAndPush(ctx, message); err != nil {
		return fmt.Errorf("config sync: push failed: %w", err)
	}
	return nil
}
