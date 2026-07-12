package llm

import "testing"

func TestAdapterRouterSelectAdapter(t *testing.T) {
	t.Parallel()

	code := &LoadedAdapter{Domain: "code", Path: "/adapters/code", Ready: true}
	debug := &LoadedAdapter{Domain: "debugging", Path: "/adapters/debug", Ready: true}
	fallback := &LoadedAdapter{Domain: "general", Path: "/adapters/general", Ready: true}
	// NotReady should never be selected even if domain matches.
	broken := &LoadedAdapter{Domain: "security", Path: "/missing", Ready: false}

	r := NewAdapterRouter(map[string]*LoadedAdapter{
		"code":      code,
		"debugging": debug,
		"security":  broken,
	}, fallback)

	if got := r.SelectAdapter("code"); got != code {
		t.Errorf("SelectAdapter(code) = %v, want code adapter", got)
	}
	if got := r.SelectAdapter("debugging"); got != debug {
		t.Errorf("SelectAdapter(debugging) = %v, want debug adapter", got)
	}
	if got := r.SelectAdapter("api_research"); got != fallback {
		t.Errorf("SelectAdapter(api_research) = %v, want fallback", got)
	}
	// Broken domain falls back rather than returning not-ready adapter.
	if got := r.SelectAdapter("security"); got != fallback {
		t.Errorf("SelectAdapter(security) = %v, want fallback (not-ready skipped)", got)
	}
}

func TestAdapterRouterNilMapAndNoFallback(t *testing.T) {
	t.Parallel()

	r := NewAdapterRouter(nil, nil)
	if got := r.SelectAdapter("code"); got != nil {
		t.Errorf("SelectAdapter on empty router = %v, want nil", got)
	}
}
