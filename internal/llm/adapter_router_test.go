package llm

import "testing"

func TestAdapterRouterSelectAdapter(t *testing.T) {
	t.Parallel()

	code := &LoadedAdapter{Domain: "code", Path: "/adapters/code"}
	debug := &LoadedAdapter{Domain: "debugging", Path: "/adapters/debug"}
	fallback := &LoadedAdapter{Domain: "general", Path: "/adapters/general"}

	r := NewAdapterRouter(map[string]*LoadedAdapter{
		"code":      code,
		"debugging": debug,
	}, fallback)

	if got := r.SelectAdapter("code"); got != code {
		t.Errorf("SelectAdapter(code) = %v, want code adapter", got)
	}
	if got := r.SelectAdapter("debugging"); got != debug {
		t.Errorf("SelectAdapter(debugging) = %v, want debug adapter", got)
	}
	if got := r.SelectAdapter("security"); got != fallback {
		t.Errorf("SelectAdapter(security) = %v, want fallback", got)
	}
}

func TestAdapterRouterNilMapAndNoFallback(t *testing.T) {
	t.Parallel()

	r := NewAdapterRouter(nil, nil)
	if got := r.SelectAdapter("code"); got != nil {
		t.Errorf("SelectAdapter on empty router = %v, want nil", got)
	}
}
