package backup

import (
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auth"
)

// testLogger keeps pool internals quiet during tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// mustAddUserFor creates a user through the real store API and fails the
// test on error.
func mustAddUserFor(t *testing.T, s *auth.Store, name string) *auth.User {
	t.Helper()
	u, err := s.AddUser(name)
	if err != nil {
		t.Fatalf("AddUser(%q): %v", name, err)
	}
	return u
}

// usersFileSnapshot reads the merged users straight from the store's
// persisted file, mirroring what MergeForeign wrote.
func usersFileSnapshot(t *testing.T, h *usersSyncHarness) ([]auth.User, error) {
	t.Helper()
	return readUsersFile(h.pool.cfg.UsersFile)
}

// usersFileSnapshotDirect reads from an explicit path (used when the test
// needs a pool-independent read).
func usersFileSnapshotDirect(t *testing.T, path string) ([]auth.User, error) {
	t.Helper()
	return readUsersFile(path)
}

// readUsersFile parses a users-store file using the same shape auth's
// saveStore writes.
func readUsersFile(path string) ([]auth.User, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var wrapper struct {
		Users []auth.User `json:"users"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, err
	}
	return wrapper.Users, nil
}

// Compile-time guards on imports that later leaf-03 follow-ups may need;
// removed automatically by vet if they become truly unused.
var (
	_ = time.Now
)
