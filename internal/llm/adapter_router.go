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

// SelectAdapter returns the adapter for the given domain, or the fallback
// if no domain-specific adapter exists. Returns nil if neither exists.
func (r *AdapterRouter) SelectAdapter(domain string) *LoadedAdapter {
	if adapter, ok := r.adapters[domain]; ok {
		return adapter
	}
	return r.fallback
}
