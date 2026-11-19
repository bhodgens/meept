package daemon

// execution_runs_rpc.go - agents.executionRuns + agents.executionHistogram
// RPC surface over the durable ExecutionStore (issue #160 follow-up).
// Direct closures (pattern: activity_rpc.go), nil-guarded per method: a
// missing store degrades THAT method to a clean RPC error, never a panic.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/activity"
	"github.com/caimlas/meept/internal/rpc"
)

// executionRunsParams mirrors the frozen C1 query contract.
type executionRunsParams struct {
	AgentID string `json:"agent_id"`
	Limit   int    `json:"limit"`
	Since   string `json:"since"` // RFC3339, optional
}

// registerExecutionRunsRPC registers the execution-history query methods.
// A nil server or nil store is a no-op (the methods then answer
// "method not found", which clients treat as the degraded state).
func registerExecutionRunsRPC(server *rpc.Server, store *activity.ExecutionStore) {
	if server == nil || store == nil {
		return
	}

	// agents.executionRuns - newest-first page of persisted runs.
	// Result: {"runs": [ExecutionRun], "total": N} where total counts
	// matching rows BEFORE the limit (paging parity with activity.query).
	server.RegisterHandler("agents.executionRuns", func(ctx context.Context, params json.RawMessage) (any, error) {
		var req executionRunsParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("agents.executionRuns: invalid parameters: %w", err)
			}
		}
		q := activity.ExecutionQuery{AgentID: req.AgentID, Limit: req.Limit}
		if req.Since != "" {
			since, err := time.Parse(time.RFC3339, req.Since)
			if err != nil {
				return nil, fmt.Errorf("agents.executionRuns: since: %w", err)
			}
			q.Since = since
		}
		runs, total, err := store.List(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("agents.executionRuns: %w", err)
		}
		if runs == nil {
			runs = []activity.ExecutionRun{}
		}
		return map[string]any{"runs": runs, "total": total}, nil
	})

	// agents.executionHistogram - 8 token cells covering the last hour for
	// one agent (7.5 minutes per cell, oldest first). Result:
	// {"agent_id": "...", "cells": [8]int}. Drives the GUI's 8-bit bars.
	server.RegisterHandler("agents.executionHistogram", func(ctx context.Context, params json.RawMessage) (any, error) {
		var req struct {
			AgentID string `json:"agent_id"`
		}
		if len(params) > 0 {
			if err := json.Unmarshal(params, &req); err != nil {
				return nil, fmt.Errorf("agents.executionHistogram: invalid parameters: %w", err)
			}
		}
		if req.AgentID == "" {
			return nil, errors.New("agents.executionHistogram: agent_id is required")
		}
		cells, err := store.Histogram(ctx, req.AgentID, time.Now())
		if err != nil {
			return nil, fmt.Errorf("agents.executionHistogram: %w", err)
		}
		return map[string]any{"agent_id": req.AgentID, "cells": cells}, nil
	})
}
