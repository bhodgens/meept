// User and API-key management commands: direct file access to the JSON5
// users store (internal/auth). Prefer no-daemon operation for v1 — the
// CLI runs on the same host as the users file.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/caimlas/meept/internal/auth"
	"github.com/spf13/cobra"
)

// expiresNever is the sentinel accepted by --expires meaning never-expiring.
const expiresNever = "never"

// usersStorePath resolves the users-file path for a command. Precedence:
// explicit --store flag, then the CLI's global state-dir
// (~/.meept unless overridden). The canonical default filename lives in the
// multiuser config leaf; see the report note in docs/plans — when that wiring
// lands this helper should prefer the configured users_file value.
func usersStorePath(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return filepath.Join(stateDir, "users.json5")
}

// bindStoreFlag registers the shared --store override flag on a user/key
// management subcommand.
func bindStoreFlag(cmd *cobra.Command, p *string) {
	cmd.Flags().StringVar(p, "store", "", "path to the users store (default ~/.meept/users.json5)")
}

// openUsersStore opens the users store at the resolved path, mapping a
// missing file to a fresh empty store so first-use works out of the box.
func openUsersStore(path string) (*auth.Store, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create state directory: %w", err)
		}
	}
	s, err := auth.NewStore(path)
	if err != nil {
		return nil, fmt.Errorf("open users store %s: %w", path, err)
	}
	return s, nil
}

// lookupUser finds a user by exact id or by unique case-insensitive name.
func lookupUser(s *auth.Store, ref string) *auth.User {
	users := s.ListUsers()
	for i := range users {
		if users[i].ID == ref {
			return &users[i]
		}
	}
	var named []*auth.User
	for i := range users {
		if strings.EqualFold(users[i].Name, ref) {
			named = append(named, &users[i])
		}
	}
	if len(named) == 1 {
		return named[0]
	}
	return nil // unknown, or ambiguous name
}

// parseExpiry parses an --expires value: RFC3339 timestamp or "never".
func parseExpiry(spec string) (*time.Time, error) {
	if spec == "" || spec == expiresNever {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, spec)
	if err != nil {
		return nil, fmt.Errorf("invalid --expires value %q: use RFC3339 (e.g. 2026-12-31T23:59:59Z) or \"never\"", spec)
	}
	return &t, nil
}

// formatExpiry renders a key expiry for display ("never" for nil).
func formatExpiry(t *time.Time) string {
	if t == nil {
		return expiresNever
	}
	return t.UTC().Format(time.RFC3339)
}

// userRow is one row of `meept users list` output.
type userRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Origin   string `json:"origin,omitempty"`
	KeyCount int    `json:"keys"`
}

// runUsersList implements `meept users list`.
func runUsersList(path string, asJSON bool) error {
	s, err := openUsersStore(path)
	if err != nil {
		return err
	}
	users := s.ListUsers()

	if asJSON {
		rows := make([]userRow, 0, len(users))
		for _, u := range users {
			rows = append(rows, userRow{ID: u.ID, Name: u.Name, Origin: u.OriginNode, KeyCount: len(u.Keys)})
		}
		if len(rows) == 0 {
			rows = []userRow{}
		}
		out, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal users json: %w", err)
		}
		fmt.Println(string(out))
		return nil
	}

	if len(users) == 0 {
		fmt.Println("no users found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tORIGIN\tKEYS")
	for _, u := range users {
		id := u.ID
		if len(id) > 40 {
			id = id[:37] + "..."
		}
		origin := u.OriginNode
		if origin == "" {
			origin = "local"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", id, u.Name, origin, len(u.Keys))
	}
	w.Flush()
	fmt.Printf("\ntotal: %d users\n", len(users))
	return nil
}

// runUsersAdd implements `meept users add <name>`.
func runUsersAdd(path, name string) error {
	s, err := openUsersStore(path)
	if err != nil {
		return err
	}
	u, err := s.AddUser(name)
	if err != nil {
		return err
	}
	fmt.Printf("created user: %s (%s)\n", u.Name, u.ID)
	fmt.Println("next: meept keys add " + u.ID + " --label <label>")
	return nil
}

// runUsersRemove implements `meept users remove <id>` with confirm prompt
// unless --yes is set.
func runUsersRemove(path, userID string, yes bool) error {
	s, err := openUsersStore(path)
	if err != nil {
		return err
	}
	u := lookupUser(s, userID)
	if u == nil {
		return fmt.Errorf("remove user: %s not found", userID)
	}
	reader := bufio.NewReader(os.Stdin)
	if !confirmDelete(reader, "user", u.ID, yes) {
		fmt.Println("aborted.")
		return nil
	}
	if err := s.RemoveUser(u.ID); err != nil {
		return err
	}
	fmt.Printf("removed user: %s (%s)\n", u.Name, u.ID)
	return nil
}

// keyRow is one row of `meept keys list` output. Raw keys are never shown;
// only id/label/expiry are listed.
type keyRow struct {
	ID        string     `json:"id"`
	Label     string     `json:"label,omitempty"`
	UserID    string     `json:"user_id"`
	UserName  string     `json:"user_name"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// runKeysAdd implements `meept keys add <user-id> [--label L] [--expires ...]`.
// The raw key is printed EXACTLY ONCE and never persisted anywhere else.
func runKeysAdd(path, userRef, label, expiresSpec string) error {
	expiresAt, err := parseExpiry(expiresSpec)
	if err != nil {
		return err
	}
	s, err := openUsersStore(path)
	if err != nil {
		return err
	}
	u := lookupUser(s, userRef)
	if u == nil {
		return fmt.Errorf("add key: unknown or ambiguous user %q", userRef)
	}
	rawKey, err := s.AddKey(u.ID, label, expiresAt)
	if err != nil {
		return err
	}
	fmt.Printf("created key for user %s (%s)\n", u.Name, u.ID)
	fmt.Println()
	fmt.Println(rawKey)
	fmt.Println()
	fmt.Println("store this api key now - it will not be shown again.")
	return nil
}

// runKeysRevoke implements `meept keys revoke <key-id>`. The target may be
// given as key id alone (searches all users) or "<user-id>/<key-id>".
func runKeysRevoke(path, keyRef string, yes bool) error {
	var userID, keyID string
	var hasUser bool
	if u, k, ok := strings.Cut(keyRef, "/"); ok {
		userID, keyID, hasUser = u, k, true
	} else {
		keyID = keyRef
	}

	s, err := openUsersStore(path)
	if err != nil {
		return err
	}
	users := s.ListUsers()

	var owner *auth.User
	var found *auth.Key
	for i := range users {
		u := users[i]
		for j := range u.Keys {
			k := u.Keys[j]
			if hasUser && u.ID == userID && k.ID == keyID {
				owner, found = &u, &k
				break
			}
			if !hasUser && k.ID == keyID {
				owner, found = &u, &k
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		return fmt.Errorf("revoke key: %s not found", keyRef)
	}

	label := found.Label
	reader := bufio.NewReader(os.Stdin)
	if !confirmDelete(reader, fmt.Sprintf("key %s of user %s", found.ID, owner.ID), labelIfSet(label), yes) {
		fmt.Println("aborted.")
		return nil
	}
	if err := s.RevokeKey(owner.ID, found.ID); err != nil {
		return err
	}
	fmt.Printf("revoked key: %s\n", found.ID)
	return nil
}

// labelIfSet renders optional labels in confirmation prompts.
func labelIfSet(label string) string {
	if label == "" {
		return "(unlabeled)"
	}
	return fmt.Sprintf("%q", label)
}

// runKeysList implements `meept keys list [user-id]`.
func runKeysList(path, userRef string, asJSON bool) error {
	s, err := openUsersStore(path)
	if err != nil {
		return err
	}
	users := s.ListUsers()
	if userRef != "" {
		u := lookupUser(s, userRef)
		if u == nil {
			return fmt.Errorf("keys list: unknown or ambiguous user %q", userRef)
		}
		users = []auth.User{*u}
	}

	var rows []keyRow
	for _, u := range users {
		for _, k := range u.Keys {
			rows = append(rows, keyRow{
				ID:        k.ID,
				Label:     k.Label,
				UserID:    u.ID,
				UserName:  u.Name,
				ExpiresAt: copyTime(k.ExpiresAt),
			})
		}
	}

	if asJSON {
		if rows == nil {
			rows = []keyRow{}
		}
		out, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal keys json: %w", err)
		}
		fmt.Println(string(out))
		return nil
	}

	if len(rows) == 0 {
		fmt.Println("no keys found.")
		return nil
	}

	now := time.Now()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KEY ID	LABEL	USER	EXPIRES")
	for _, r := range rows {
		keyID := r.ID
		if len(keyID) > 40 {
			keyID = keyID[:37] + "..."
		}
		expired := r.ExpiresAt != nil && now.After(*r.ExpiresAt)
		expiry := formatExpiry(r.ExpiresAt)
		if expired {
			expiry += " (expired)"
		}
		fmt.Fprintf(w, "%s	%s	%s	%s\n",
			keyID,
			r.Label,
			strings.TrimSpace(r.UserName+" ("+r.UserID+")"),
			expiry)
	}
	w.Flush()
	fmt.Printf("\ntotal: %d keys\n", len(rows))
	return nil
}

// copyTime returns a defensive copy of a nullable timestamp.
func copyTime(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}

// ---------------------------------------------------------------------------
// cobra trees
// ---------------------------------------------------------------------------

func newUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "manage daemon users (multi-user mode)",
		Long: `Manage users for the daemon's multi-user mode.

Users hold API keys and are stored in ~/.meept/users.json5. These commands
edit the store file directly on this host. Multi-user auth itself is opt-in:
enable it with multiuser.enabled=true in the daemon config.

Examples:
  meept users list
  meept users add alice
  meept users remove <user-id> [--yes]`,
	}
	cmd.AddCommand(newUsersListCmd())
	cmd.AddCommand(newUsersAddCmd())
	cmd.AddCommand(newUsersRemoveCmd())
	return cmd
}

func newUsersListCmd() *cobra.Command {
	var (
		outputJSON bool
		storePath  string
	)
	cmd := &cobra.Command{
		Use:   cmdList,
		Short: "list users",
		Long:  "list all users in the local users store, local and cluster-pooled.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersList(usersStorePath(storePath), outputJSON)
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	bindStoreFlag(cmd, &storePath)
	return cmd
}

func newUsersAddCmd() *cobra.Command {
	var storePath string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "add a user",
		Long:  "create a user with the given display name. names must be unique.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersAdd(usersStorePath(storePath), args[0])
		},
	}
	bindStoreFlag(cmd, &storePath)
	return cmd
}

func newUsersRemoveCmd() *cobra.Command {
	var (
		yes       bool
		storePath string
	)
	cmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "remove a user and their keys",
		Long:  "delete a local user by id and revoke every key they hold. requires confirmation unless --yes is passed.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsersRemove(usersStorePath(storePath), args[0], yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	bindStoreFlag(cmd, &storePath)
	return cmd
}

func newKeysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "manage user api keys (multi-user mode)",
		Long: `Manage API keys held by users.

keys authenticate requests in multi-user mode. raw keys are shown once at
creation and only sha256 hashes are stored.

Examples:
  meept keys add <user-id> --label laptop --expires 2027-01-01T00:00:00Z
  meept keys revoke <key-id>
  meept keys list [user-id]`,
	}
	cmd.AddCommand(newKeysAddCmd())
	cmd.AddCommand(newKeysRevokeCmd())
	cmd.AddCommand(newKeysListCmd())
	return cmd
}

func newKeysAddCmd() *cobra.Command {
	var (
		label     string
		expires   string
		storePath string
	)
	cmd := &cobra.Command{
		Use:   "add <user-id>",
		Short: "add an api key for a user",
		Long: `generate a new api key for a user. the raw key prints exactly once —
copy it immediately; only its hash is stored.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKeysAdd(usersStorePath(storePath), args[0], label, expires)
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "human-readable label for the key")
	cmd.Flags().StringVar(&expires, "expires", "", "expiry as RFC3339 (e.g. 2027-01-01T00:00:00Z) or \"never\"")
	bindStoreFlag(cmd, &storePath)
	return cmd
}

func newKeysRevokeCmd() *cobra.Command {
	var (
		yes       bool
		storePath string
	)
	cmd := &cobra.Command{
		Use:   "revoke <key-id>",
		Short: "revoke an api key",
		Long: `revoke a key by id across all users, or scope to one user with
<user-id>/<key-id>. revocation takes effect for running daemons once peer
sync propagates the change (or at next restart).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKeysRevoke(usersStorePath(storePath), args[0], yes)
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt")
	bindStoreFlag(cmd, &storePath)
	return cmd
}

func newKeysListCmd() *cobra.Command {
	var (
		outputJSON bool
		storePath  string
	)
	cmd := &cobra.Command{
		Use:   "list [user-id]",
		Short: "list api keys",
		Long:  "list keys with id/label/expiry — raw key material is never displayed.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := ""
			if len(args) == 1 {
				ref = args[0]
			}
			return runKeysList(usersStorePath(storePath), ref, outputJSON)
		},
	}
	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	bindStoreFlag(cmd, &storePath)
	return cmd
}
