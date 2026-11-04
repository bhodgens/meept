package memory

// Scope-aware recall options (01-scope-schema, C2 + dated addendum Flag 3).
//
// The options are ADDITIVE: Search / GetRelevantContext callers that do not
// pass them behave byte-identically to the pre-scoping behavior (no
// filtering at all). Callers that pass WithScopeFilter get the C2 predicate:
//
//	recallable iff
//	  row scope == global, OR
//	  row scope == project AND projectID matches the row's project_id, OR
//	  row scope == user AND userID matches the row's user_id
//	    (an empty filter userID matches ONLY when multiuser is disabled).
//
// Flag 3 (fail-closed): when WithMultiUserEnabled(true) is set and the
// resolved userID is empty, user-scope rows are NOT recallable — an identity
// resolution gap under multiuser must never widen recall.

// searchOptions carries the variadic option state threaded through the
// Manager search surfaces.
type searchOptions struct {
	scope *scopeRecallFilter
}

// scopeRecallFilter is the resolved C2 predicate. nil == unfiltered
// (legacy behavior).
type scopeRecallFilter struct {
	maxScope  Scope
	projectID string
	userID    string
	multiUser bool
}

// SearchOption mutates the optional search state. Options are variadic
// trailing arguments on Manager.Search / GetRelevantContext (C2 contract:
// existing callers compile unchanged).
type SearchOption func(*searchOptions)

// WithScopeFilter restricts recall to rows at or below maxScope that match
// the caller's project/user identity, per the C2 predicate. An empty
// projectID means the caller is unbound to any project: project-scoped rows
// are hidden. An empty userID matches user-scope rows ONLY when multiuser
// is disabled (WithMultiUserEnabled(false), the default when the option is
// absent); under WithMultiUserEnabled(true) an empty userID hides user rows
// (Flag 3 fail-closed).
func WithScopeFilter(maxScope Scope, projectID, userID string) SearchOption {
	return func(o *searchOptions) {
		o.scope = &scopeRecallFilter{
			maxScope:  maxScope,
			projectID: projectID,
			userID:    userID,
		}
	}
}

// WithMultiUserEnabled carries the multiuser-enabled bit into the scope
// predicate (deviation pin from gate 7b775b70: additive SearchOption rather
// than a fourth WithScopeFilter parameter, keeping the frozen C2 signature
// verbatim). With multiUser=true and an empty filter userID, user-scope
// rows are NOT recallable (Flag 3: fail closed). Absent option = disabled
// (single-user install: empty userID matches).
func WithMultiUserEnabled(multiUser bool) SearchOption {
	return func(o *searchOptions) {
		if o.scope != nil {
			o.scope.multiUser = multiUser
		}
	}
}

// resolveScopeOptions folds the variadic options into the effective filter
// (nil = unfiltered legacy recall).
func resolveScopeOptions(opts []SearchOption) *scopeRecallFilter {
	var so searchOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&so)
		}
	}
	return so.scope
}

// visible reports whether one memory row passes the C2 predicate.
//
// maxScope is the CALLER's operating scope (the session's purview), per the
// committed gate pins (TestScopeFilterMatrix):
//   - global rows are always visible;
//   - a project-scope caller sees project rows bound to its projectID;
//   - a user-scope caller sees ALL project rows (projects sit beneath the
//     user's purview; no user->project binding exists) plus user rows that
//     match its userID — an empty filter userID matches ONLY when multiuser
//     is disabled, and multiuser-enabled + empty userID fails CLOSED
//     (Flag 3);
//   - a global-scope caller sees only global rows (no project/user identity
//     to match).
func (f *scopeRecallFilter) visible(mem Memory) bool {
	scope := mem.Scope
	if scope == "" {
		scope = scopeFromMetadata(mem.Metadata)
	}
	switch scope {
	case ScopeGlobal:
		// Global rows are always visible.
		return true
	case ScopeProject:
		switch f.maxScope {
		case ScopeProject:
			// Project caller: only rows bound to its own project.
			if f.projectID == "" {
				return false
			}
			return scopeRowOwner(mem.Metadata, "project_id") == f.projectID
		case ScopeUser:
			// User caller: all project facts are within its purview.
			return true
		default:
			// Global (or unknown) caller: no project identity.
			return false
		}
	case ScopeUser:
		// User rows are only ever visible to user-scope callers.
		if f.maxScope != ScopeUser {
			return false
		}
		// Empty filter userID matches ONLY when multiuser is disabled
		// (single-user install); multiuser enabled + empty userID fails
		// CLOSED (Flag 3).
		if f.multiUser && f.userID == "" {
			return false
		}
		if f.userID == "" {
			return true
		}
		return scopeRowOwner(mem.Metadata, "user_id") == f.userID
	default:
		// Unknown scope value: treat as global (legacy/default backfill
		// vocabulary). Never hide rows whose scope the writer did not set.
		return true
	}
}

// scopeRowOwner reads the identity key (project_id / user_id) a scope row
// was stamped with at store time.
func scopeRowOwner(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta[key].(string); ok {
		return v
	}
	return ""
}

// filterScopeResults applies the predicate to a result slice in place
// semantics (returns the filtered slice). A nil filter returns the input
// unchanged — the byte-identical legacy path.
func filterScopeResults(results []MemoryResult, f *scopeRecallFilter) []MemoryResult {
	if f == nil {
		return results
	}
	filtered := results[:0]
	for _, r := range results {
		if f.visible(r.Memory) {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
