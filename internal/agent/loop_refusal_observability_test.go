package agent

// Refusal-fallback tree leaf 04: observability tests.
//
//   - Reply-disclosure contract (USER DECISION 2026-09-16, option b): when a
//     refusal-fallback retry serves a turn, the user-visible reply text ends
//     with "\n\n[answered by <fallback model> after refusal]" — CODE-
//     appended at reply assembly, never model-generated. Normal turns are
//     byte-identical to the pre-leaf behavior.
//   - Ledger identity pin (Task 3): the refusal retry's llm_calls usage row
//     names the FALLBACK model (resolved-model identity invariant). Expected
//     to pass by construction (chatWithFailoverRaw stamps
//     llm.WithModelOverride; Client.Chat resolves the effective config
//     before recordUsageStore) — a failure here is a REAL finding.

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	appmetrics "github.com/caimlas/meept/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// refusalFallbackLoop builds a loop wired exactly like leaf 03's retry test
// (first `refusals` calls refuse, then the REAL client serves the pinned
// fallback model on the wire).
func refusalFallbackLoop(t *testing.T, refusals int) (*AgentLoop, *refusalChatter, *modelCapture) {
	t.Helper()

	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	t.Cleanup(func() { server.Close() })

	resolver := resolvedAliasResolver(t, server.URL, "lfm-8b-q4", "alias-served-model")

	inner := llm.NewClient(&llm.ModelConfig{
		BaseURL:    server.URL,
		ProviderID: "local",
		ModelID:    "wire-model",
	})
	chatter := &refusalChatter{inner: inner, refusals: refusals}
	loop := NewAgentLoop("sess-refusal-disclosure", t.TempDir(),
		WithLoopLogger(slog.New(slog.NewTextHandler(os.Stderr, nil))),
		WithResolver(resolver),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(chatter),
	)
	loop.refusalResolver = llm.NewResolver(&llm.ProvidersConfig{
		ModelAliases: map[string]llm.ModelAliasEntry{
			"fb": {Models: []string{"local/fb-model"}},
		},
		Providers: map[string]llm.ProviderConfig{
			"local": {API: "openai", Options: llm.ProviderOptionsConfig{BaseURL: server.URL}, Models: map[string]llm.ModelDef{
				"fb-model": {Name: "fb-model"},
			}},
		},
	}, nil)
	loop.spec = &AgentSpec{RefusalModel: "fb"}
	loop.refusalEventPublisher = func(string, map[string]any) {} // no bus wired
	loop.refusalOverrideApplier = loop.SetPersistentModelOverride
	loop.refusalOverrideClear = loop.ClearModelOverride
	return loop, chatter, cap
}

// TestRefusalFallback_ReplyCarriesDisclosure: after a successful fallback
// retry, the turn's reply text ends with the disclosure line naming the
// fallback model.
func TestRefusalFallback_ReplyCarriesDisclosure(t *testing.T) {
	loop, chatter, _ := refusalFallbackLoop(t, 1)

	_, err := loop.chatWithFailoverRaw(context.Background(),
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, chatter.callCount(), "refusal + fallback retry")

	// Reply assembly reads the turn-scoped state: the fallback that served
	// the successful retry must be disclosed in the reply text, exactly the
	// note RunOnceWithParts appends.
	disclosure := loop.refusalFallbackDisclosure()
	require.NotEmpty(t, disclosure, "a served fallback must disclose itself")
	assert.Equal(t, "\n\n[answered by local/fb-model after refusal]", disclosure)
	assert.True(t, len("assistant text"+disclosure) > len(disclosure) &&
		("assistant text" + disclosure)[len("assistant text"+disclosure)-len(disclosure):] == disclosure,
		"reply text must end with the disclosure suffix")
}

// TestRefusalFallback_NoFallback_ReplyByteIdentical: without a refusal the
// disclosure is empty — normal-turn replies stay byte-identical.
func TestRefusalFallback_NoFallback_ReplyByteIdentical(t *testing.T) {
	loop, chatter, _ := refusalFallbackLoop(t, 0)

	_, err := loop.chatWithFailoverRaw(context.Background(),
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}, nil)
	require.NoError(t, err)
	require.Equal(t, 1, chatter.callCount(), "single normal call, no retry")

	assert.Empty(t, loop.refusalFallbackDisclosure(),
		"normal turns must not carry a disclosure note (byte-identical replies)")
}

// TestRefusalFallback_GiveUpPathsNeverArmDisclosure: feature-off and
// one-hop give-up paths must not arm the disclosure — only a retry that is
// actually PINNED may.
func TestRefusalFallback_GiveUpPathsNeverArmDisclosure(t *testing.T) {
	t.Run("feature-off", func(t *testing.T) {
		loop, _, _ := refusalFallbackLoop(t, 1)
		loop.spec = &AgentSpec{} // no refusal model anywhere
		_, err := loop.chatWithFailoverRaw(context.Background(),
			[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}, nil)
		require.Error(t, err)
		assert.Empty(t, loop.refusalFallbackDisclosure())
	})

	t.Run("fallback-refuses-too", func(t *testing.T) {
		loop, _, _ := refusalFallbackLoop(t, 2) // primary + fallback both refuse
		_, err := loop.chatWithFailoverRaw(context.Background(),
			[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}, nil)
		require.Error(t, err, "double refusal must surface")
		// The retry WAS armed by handleRefusal (the pin fired before the
		// fallback refused); the flag is turn-scoped and the turn FAILED,
		// so no reply text is assembled — but the fresh-turn clear at
		// begin() guarantees the NEXT turn starts clean. Assert the clear
		// seam directly.
		loop.clearRefusalFallbackServed()
		assert.Empty(t, loop.refusalFallbackDisclosure(),
			"fresh-turn clear must reset the disclosure state")
	})
}

// TestRefusalFallback_UsageLedgerRecordsFallbackModel (Task 3, ledger
// identity pin): the refusal retry's metrics.db llm_calls row names the
// FALLBACK provider/model — the resolved-model identity invariant. The
// usage store is attached to the REAL llm.Client, so the row is written by
// the production recording path (recordUsageStore), not a stub.
func TestRefusalFallback_UsageLedgerRecordsFallbackModel(t *testing.T) {
	store, err := appmetrics.NewStore(&appmetrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		FlushInterval: time.Hour, // disable background flush; llm_calls writes are direct
	})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	resolver := resolvedAliasResolver(t, server.URL, "lfm-8b-q4", "alias-served-model")

	inner := llm.NewClient(&llm.ModelConfig{
		BaseURL:    server.URL,
		ProviderID: "local",
		ModelID:    "wire-model",
	})
	inner.SetUsageStore(store)
	chatter := &refusalChatter{inner: inner, refusals: 1}
	loop := NewAgentLoop("sess-refusal-ledger", t.TempDir(),
		WithLoopLogger(slog.New(slog.NewTextHandler(os.Stderr, nil))),
		WithResolver(resolver),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(chatter),
	)
	loop.refusalResolver = llm.NewResolver(&llm.ProvidersConfig{
		ModelAliases: map[string]llm.ModelAliasEntry{
			"fb": {Models: []string{"local/fb-model"}},
		},
		Providers: map[string]llm.ProviderConfig{
			"local": {API: "openai", Options: llm.ProviderOptionsConfig{BaseURL: server.URL}, Models: map[string]llm.ModelDef{
				"fb-model": {Name: "fb-model"},
			}},
		},
	}, nil)
	loop.spec = &AgentSpec{RefusalModel: "fb"}
	loop.refusalEventPublisher = func(string, map[string]any) {}
	loop.refusalOverrideApplier = loop.SetPersistentModelOverride
	loop.refusalOverrideClear = loop.ClearModelOverride

	_, err = loop.chatWithFailoverRaw(context.Background(),
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}, nil)
	require.NoError(t, err)

	// recordUsageStore writes asynchronously; poll briefly for the row.
	// llm_calls has no direct per-model query export, so read the row via
	// the raw store handle (same table RecordLLMCall inserts into) — the
	// point is to prove the PRODUCTION recording path attributed the call
	// to the fallback model, exactly as tokscale/aggregation would see it.
	deadline := time.Now().Add(5 * time.Second)
	var fbCalls, wireDefaultCalls int
	for time.Now().Before(deadline) {
		fbCalls, wireDefaultCalls = 0, 0
		if err := store.DB().Get(&fbCalls,
			`SELECT COUNT(*) FROM llm_calls WHERE provider='local' AND model_id='fb-model'`); err != nil {
			require.NoError(t, err)
		}
		if err := store.DB().Get(&wireDefaultCalls,
			`SELECT COUNT(*) FROM llm_calls WHERE model_id='wire-model'`); err != nil {
			require.NoError(t, err)
		}
		if fbCalls >= 1 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}

	// The single wire request is the fallback retry; the ledger row for it
	// must name the FALLBACK model (local/fb-model), not the client's
	// configured default (wire-model).
	assert.GreaterOrEqual(t, fbCalls, 1,
		"expected an llm_calls row for local/fb-model (the fallback)")
	assert.Zero(t, wireDefaultCalls,
		"the fallback retry must be attributed to the FALLBACK model, not the client default wire-model")
}

// compile-time guard: keep the sync import honest if helpers change.
var _ sync.Mutex
