package tui

// Multi-user awareness modal (client-tooling leaf).
//
// Parity rule: the TUI offers whatever the Flutter GUI offers. Today neither
// surface can manage users because the daemon exposes no management path to
// clients — there are no users.* RPC methods and the "status" payload carries
// no multi-user fields (only the daemon startup log mentions them). Until a
// wire surface exists, every action here renders disabled with its CLI
// equivalent so operators always land on working commands rather than dead
// buttons.

// UsersModal displays multi-user awareness information for the current
// daemon. v1 is read-only guidance: users and keys live in the daemon-side
// store (~/.meept/users.json5) and are managed via the `meept users` CLI,
// which reads the same file directly on the daemon host.
type UsersModal struct {
	*Modal
}

// NewUsersModal creates the users awareness modal.
func NewUsersModal(styles *Styles) *UsersModal {
	m := &UsersModal{Modal: NewModal("users", styles)}
	m.width = 90
	m.SetItems(UsersModalItems())
	return m
}

// UsersModalItems builds the guidance rows shown inside the users modal.
// Every row is intentionally Disabled: none of these actions have a client
// callable path yet, and acting-as-if otherwise would lie to the operator.
//
// When a future leaf exposes users.* RPC methods or identity-aware status,
// flip rows to enabled and attach real handlers; keep the row order stable
// so muscle memory survives the transition.
func UsersModalItems() []ModalItem {
	return []ModalItem{
		{
			Key:         "i",
			Label:       "identity",
			Description: "clients connect to the shared daemon; identities live server-side",
			Disabled:    true,
		},
		{
			Key:         "l",
			Label:       "list users and keys",
			Description: "cli: meept users list · meept users keys list",
			Disabled:    true,
		},
		{
			Key:         "u",
			Label:       "add user",
			Description: "cli: meept users add <name>",
			Disabled:    true,
		},
		{
			Key:         "k",
			Label:       "add api key",
			Description: "cli: meept users keys add <user-id> [--label s] [--expires rfc3339]",
			Disabled:    true,
		},
		{
			Key:         "r",
			Label:       "revoke key",
			Description: "cli: meept users keys revoke <key-id>",
			Disabled:    true,
		},
		{
			Key:         "e",
			Label:       "multi-user disabled?",
			Description: "set multiuser.enabled = true in meept.json5 and restart the daemon",
			Disabled:    true,
		},
	}
}
