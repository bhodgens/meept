package pii

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSwapStore_PersistenceAcrossReopen is the durable proof the leaf
// requires: a row written before Close resolves identically after the
// store is reopened from the same directory.
func TestSwapStore_PersistenceAcrossReopen(t *testing.T) {
	dir := t.TempDir()

	st, err := NewSwapStore(dir)
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	const (
		sess  = "sess-a"
		tok   = "[PII:11111111-2222-4333-8444-555555555555]"
		val   = "alice@example.com"
		class = ClassEmailAddress
	)
	if err := st.Put(sess, tok, val, class); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	st2, err := NewSwapStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()

	got, ok := st2.Resolve(sess, tok)
	if !ok || got != val {
		t.Fatalf("Resolve after reopen = (%q, %v); want (%q, true)", got, ok, val)
	}
}

// TestSwapStore_CrossSessionTokenUntouched covers leaf T2: a token
// belonging to sessA is left as a literal token in sessB's text.
func TestSwapStore_CrossSessionTokenUntouched(t *testing.T) {
	st, err := NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	defer st.Close()

	tokA := "[PII:aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee]"
	if err := st.Put("sess-a", tokA, "alice@example.com", ClassEmailAddress); err != nil {
		t.Fatalf("Put: %v", err)
	}

	text := "reply to " + tokA + " please"
	restored, count := st.ResolveForRestore("sess-b", text)
	if count != 0 || restored != text {
		t.Fatalf("cross-session restore leaked: count=%d restored=%q", count, restored)
	}
}

// TestSwapStore_ResolveForRestoreReplacesAll covers leaf T2: ALL tokens
// belonging to sessionID in text are replaced and the count is exact.
func TestSwapStore_ResolveForRestoreReplacesAll(t *testing.T) {
	st, err := NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	defer st.Close()

	const (
		tokA = "[PII:11111111-1111-4111-8111-111111111111]"
		tokB = "[PII:22222222-2222-4222-8222-222222222222]"
	)
	if err := st.Put("sess-a", tokA, "alice@example.com", ClassEmailAddress); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := st.Put("sess-a", tokB, "555-0100", ClassPhoneNumber); err != nil {
		t.Fatalf("Put: %v", err)
	}

	text := tokA + " and " + tokB + " and " + tokA
	restored, count := st.ResolveForRestore("sess-a", text)
	want := "alice@example.com and 555-0100 and alice@example.com"
	if count != 3 {
		t.Fatalf("count = %d; want 3", count)
	}
	if restored != want {
		t.Fatalf("restored = %q; want %q", restored, want)
	}
}

// TestSwapStore_UnknownTokenUntouched pins that only session-owned,
// known tokens are replaced; unknown [PII:...] shapes pass through.
func TestSwapStore_UnknownTokenUntouched(t *testing.T) {
	st, err := NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	defer st.Close()

	text := "nothing to see [PII:99999999-9999-4999-8999-999999999999] here"
	restored, count := st.ResolveForRestore("sess-a", text)
	if count != 0 || restored != text {
		t.Fatalf("unknown token touched: count=%d restored=%q", count, restored)
	}
}

// TestSwapStore_DirCreatedWithRestrictedPerms covers the leaf note:
// NewSwapStore creates a missing dir with MkdirAll 0700.
func TestSwapStore_DirCreatedWithRestrictedPerms(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "pii")
	st, err := NewSwapStore(dir)
	if err != nil {
		t.Fatalf("NewSwapStore into missing dir: %v", err)
	}
	defer st.Close()

	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o700 {
		t.Fatalf("dir perms = %o; want 700", got)
	}
}

// TestSwapStore_ErrorsCarryNoPlaintext pins the value discipline: error
// strings never contain swap values (they wrap indices/ids only).
func TestSwapStore_ErrorsCarryNoPlaintext(t *testing.T) {
	st, err := NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	const secret = "alice@example.com"
	err = st.Put("sess-a", "[PII:11111111-1111-4111-8111-111111111111]", secret, ClassEmailAddress)
	if err == nil {
		t.Fatal("Put on closed store must fail")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error string carries plaintext value: %q", err.Error())
	}
}

// TestSwapStore_TokenForDeterministic pins the in-session determinism
// index used by the Swapper (D3): same value -> same token per session.
func TestSwapStore_TokenForDeterministic(t *testing.T) {
	st, err := NewSwapStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	defer st.Close()

	tok := "[PII:33333333-3333-4333-8333-333333333333]"
	if err := st.Put("sess-a", tok, "alice@example.com", ClassEmailAddress); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := st.TokenFor("sess-a", ClassEmailAddress, "alice@example.com")
	if !ok || got != tok {
		t.Fatalf("TokenFor = (%q, %v); want (%q, true)", got, ok, tok)
	}
	// Other sessions never see it.
	if _, ok := st.TokenFor("sess-b", ClassEmailAddress, "alice@example.com"); ok {
		t.Fatal("TokenFor crossed the session boundary")
	}
}

// T-restore-window (leaf 02, research D4): a positive restore window makes
// rows expire after the window; the Resolve paths then refuse them exactly
// like foreign tokens. 0 (default) = session lifetime.
func TestSwapStoreRestoreWindowExpiry(t *testing.T) {
	dir := t.TempDir()
	st, err := NewSwapStore(dir)
	if err != nil {
		t.Fatalf("NewSwapStore: %v", err)
	}
	defer st.Close()

	// Default: no expiry.
	if err := st.Put("s1", "[PII:tok-a]", "a@example.com", "EMAIL_ADDRESS"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := st.Resolve("s1", "[PII:tok-a]"); !ok {
		t.Fatal("row refused with no TTL set")
	}

	// Arm a 1-second window and Put a fresh row.
	st.SetRestoreWindow(time.Second)
	if err := st.Put("s1", "[PII:tok-b]", "b@example.com", "EMAIL_ADDRESS"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, ok := st.Resolve("s1", "[PII:tok-b]"); !ok {
		t.Fatal("fresh row inside the window refused")
	}
	restored, n := st.ResolveForRestore("s1", "mail [PII:tok-b] now")
	if n != 1 || !strings.Contains(restored, "b@example.com") {
		t.Fatalf("in-window restore failed: %q (count=%d)", restored, n)
	}

	// Force-expire: backdate the row past the window.
	st.mu.Lock()
	row := st.mem[storeKey("s1", "[PII:tok-b]")]
	row.Expiry = time.Now().Unix() - 1
	st.mem[storeKey("s1", "[PII:tok-b]")] = row
	st.mu.Unlock()

	if _, ok := st.Resolve("s1", "[PII:tok-b]"); ok {
		t.Fatal("expired row resolved")
	}
	restored, n = st.ResolveForRestore("s1", "mail [PII:tok-b] now")
	if n != 0 || restored != "mail [PII:tok-b] now" {
		t.Fatalf("expired token restored anyway: %q (count=%d)", restored, n)
	}

	// Disabling the window (zero) un-stamps future rows.
	st.SetRestoreWindow(0)
	if err := st.Put("s1", "[PII:tok-c]", "c@example.com", "EMAIL_ADDRESS"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	st.mu.RLock()
	if st.mem[storeKey("s1", "[PII:tok-c]")].Expiry != 0 {
		st.mu.RUnlock()
		t.Fatal("zero window stamped an expiry")
	}
	st.mu.RUnlock()
}
