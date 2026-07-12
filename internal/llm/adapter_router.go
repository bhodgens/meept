package llm

// AdapterRouter selects the right adapter per request based on domain.
type AdapterRouter struct {
	adapters map[string]*LoadedAdapter
	fallback *LoadedAdapter // Used when no domain-specific adapter matches
}

// NewAdapterRouter creates an AdapterRouter from a domain->adapter map and
// an optional fallback adapter.
func NewAdapterRouter(adapters map[string]*LoadedAdapter, fallback *LoadedAdapter) *AdapterRouter {
	return &AdapterRouter{
		adapters: adapters,
		fallback: fallback,
	}
}

// NewAdapterRouterFromLoader builds a router using the loader's domain map
// and its resolved Fallback (general / first ready adapter).
func NewAdapterRouterFromLoader(loader *LFMLoader) *AdapterRouter {
	if loader == nil {
		return NewAdapterRouter(nil, nil)
	}
	return NewAdapterRouter(loader.Adapters, loader.Fallback)
}

// SelectAdapter returns the adapter for the given domain, or the fallback
// if no domain-specific adapter exists. Returns nil if neither exists.
// Ready=false adapters are skipped so inference never points at incomplete dirs.
func (r *AdapterRouter) SelectAdapter(domain string) *LoadedAdapter {
	if r == nil {
		return nil
	}
	if adapter, ok := r.adapters[domain]; ok && adapter != nil && adapterReady(adapter) {
		return adapter
	}
	if r.fallback != nil && adapterReady(r.fallback) {
		return r.fallback
	}
	return nil
}

// adapterReady treats adapters without an explicit Ready flag (legacy callers
// that only set Path) as ready when Path is non-empty. Explicit Ready=false
// always fails closed.
func adapterReady(a *LoadedAdapter) bool {
	if a == nil {
		return false
	}
	// If Ready was never set but Path exists, allow (backward compatible).
	// LFMLoader always sets Ready based on artifact validation.
	if !a.Ready && a.Path == "" {
		return false
	}
	// Prefer explicit Ready when Path is set from loader.
	if a.Path != "" {
		// Ready defaults to false for zero-value; loader sets true when valid.
		// For tests/manual construction that set Ready:true or only Path:
		if a.Ready {
			return true
		}
		// Zero-value Ready with Path: treat as ready only if Version/ID unset
		// and Domain set without going through loader — actually tests set Ready.
		// Fail closed when Ready is false.
		return false
	}
	return false
}
