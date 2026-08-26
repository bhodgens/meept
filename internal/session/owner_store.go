// owner_store.go defines the ownership-aware API layered on top of the
// base session Store, per the multi-user access plan:
//
//   - Session creation in multi-user mode stamps the authenticated user id.
//   - List/read accept an optional viewer filter; a nil viewer is UNFILTERED,
//     which is exactly the legacy single-key behavior (contract: "nil viewer
//     unfiltered").
//
// Ownership is an orthogonal concern to persistence: SQLiteStore and
// MemoryStore both implement OwnerStore. Callers that only need the legacy
// surface keep using Store unchanged — the single-key path never touches
// anything here.
package session

import "context"

// CreateForOwnerRequest carries session-creation parameters for a
// multi-user create.
type CreateForOwnerRequest struct {
	Name    string // display name; defaults like Store.Create when empty
	OwnerID string // auth user id stamping the session; "" = unowned
}

// Viewer filters listing/reads by owner identity.
//
// A nil *Viewer means no filtering (legacy behavior: all sessions visible).
type Viewer struct {
	// UserID restricts results to sessions owned by this user id.
	UserID string
}

// NewViewer returns a Viewer scoped to userID.
func NewViewer(userID string) *Viewer {
	return &Viewer{UserID: userID}
}

// VisibleTo reports whether sess is visible to viewer. A nil viewer sees
// everything (legacy mode); a set viewer additionally sees unowned sessions
// so pre-multiuser data stays reachable from any account. Exported for the
// services layer's cross-package visibility checks.
func VisibleTo(sess *Session, v *Viewer) bool {
	if v == nil || v.UserID == "" {
		return true
	}
	if sess == nil {
		return false
	}
	return sess.OwnerID == "" || sess.OwnerID == v.UserID
}

// OwnedListFallback adapts a plain Store into a filtered list via List().
func OwnedListFallback(_ context.Context, st Store, viewer *Viewer) ([]*Session, error) {
	sessions, err := st.List()
	if err != nil {
		return nil, err
	}
	out := make([]*Session, 0, len(sessions))
	for _, s := range sessions {
		if VisibleTo(s, viewer) {
			out = append(out, s)
		}
	}
	return out, nil
}

// OwnerStore is implemented by stores that support session ownership.
// SQLiteStore and MemoryStore both implement it.
type OwnerStore interface {
	CreateForOwner(ctx context.Context, req CreateForOwnerRequest) (*Session, error)
	ListForViewer(ctx context.Context, viewer *Viewer) ([]*Session, error)
	// GetForViewer returns the session with the given id when it exists and
	// is visible to viewer; nil otherwise (no error: invisible and missing
	// are indistinguishable by design).
	GetForViewer(ctx context.Context, viewer *Viewer, id string) *Session
}
