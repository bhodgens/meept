// Kanban sidecar dev-key token seed (plans/tui-gui-consistency-fixes
// 01-backend/03-sidecar-devkey): with the sidecar default-on, the GUI ships
// $MEEPT_HOME/dev_key as its bearer token to the sidecar. The sidecar's
// TokenStore starts empty and answers 401 to everything until a browser
// pairs. Granting the EXISTING per-installation dev key the three board
// grants before spawn lets a default install open the board without a
// separate pairing handshake.
//
// The seed is the existing installation credential, not a new secret. It
// carries the same trust as the loopback daemon HTTP path. No anonymous or
// default token is introduced, and the KEY VALUE IS NEVER LOGGED.

package daemon

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/caimlas/meept/internal/kanbansvc"
	"github.com/caimlas/meept/pkg/constants"
)

// seedKanbanDevKeyToken grants the per-installation dev key full board
// access in the shared sidecar token store ($MEEPT_HOME/kanban/tokens.json,
// the same file the child loads). TokenStore.Grant replaces only this
// token's grant set, so re-running the daemon is idempotent and any tokens
// minted through the pairing lane are left untouched.
//
// The key is read via constants.DevAPIKey(), which generates + persists
// $MEEPT_HOME/dev_key on first boot. Never log its value: the only log
// output is a presence-only message with no attributes.
func seedKanbanDevKeyToken(logger *slog.Logger) error {
	key := constants.DevAPIKey()
	if key == "" {
		return errors.New("kanban dev-key seed: empty dev key")
	}
	store, err := kanbansvc.NewTokenStore("")
	if err != nil {
		return fmt.Errorf("kanban dev-key seed: open token store: %w", err)
	}
	if err := store.Grant(key, kanbansvc.GrantRead, kanbansvc.GrantMutate, kanbansvc.GrantTakeover); err != nil {
		return fmt.Errorf("kanban dev-key seed: grant board access: %w", err)
	}
	if logger != nil {
		// Presence only — the key value is never logged.
		logger.Debug("kanban sidecar: dev-key token seeded in shared store")
	}
	return nil
}
