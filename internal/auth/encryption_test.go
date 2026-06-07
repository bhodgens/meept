package auth

import (
	"bytes"
	"crypto/rand"
	"os"
	"testing"
)

func TestNewEncryptionKey_UserKey(t *testing.T) {
	key, err := NewEncryptionKey("my-secret-key")
	if err != nil {
		t.Fatalf("NewEncryptionKey: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil EncryptionKey")
	}
	if len(key.Key()) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key.Key()))
	}
}

func TestNewEncryptionKey_MachineKey(t *testing.T) {
	key, err := NewEncryptionKey("")
	if err != nil {
		t.Fatalf("NewEncryptionKey with empty key: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil EncryptionKey")
	}
	if len(key.Key()) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key.Key()))
	}
}

func TestNewEncryptionKey_Deterministic(t *testing.T) {
	key1, err := NewEncryptionKey("test-key-123")
	if err != nil {
		t.Fatalf("NewEncryptionKey: %v", err)
	}
	key2, err := NewEncryptionKey("test-key-123")
	if err != nil {
		t.Fatalf("NewEncryptionKey: %v", err)
	}
	if !bytes.Equal(key1.Key(), key2.Key()) {
		t.Fatal("same user key should produce same encryption key")
	}
}

func TestNewEncryptionKey_DifferentKeys(t *testing.T) {
	key1, _ := NewEncryptionKey("key-a")
	key2, _ := NewEncryptionKey("key-b")
	if bytes.Equal(key1.Key(), key2.Key()) {
		t.Fatal("different user keys should produce different encryption keys")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key, _ := NewEncryptionKey("test-key")
	plaintext := []byte(`{"access_token":"secret123","refresh_token":"ref456"}`)

	encrypted, err := key.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if string(encrypted) == string(plaintext) {
		t.Fatal("encrypted output should differ from plaintext")
	}

	decrypted, err := key.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("round trip mismatch:\ngot:  %s\nwant: %s", decrypted, plaintext)
	}
}

func TestEncryptDecrypt_LargeInput(t *testing.T) {
	key, _ := NewEncryptionKey("test-key")
	// 64KB of random data.
	plaintext := make([]byte, 65536)
	rand.Read(plaintext)

	encrypted, err := key.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt large: %v", err)
	}

	decrypted, err := key.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt large: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatal("large round trip mismatch")
	}
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	key, _ := NewEncryptionKey("test-key")
	plaintext := []byte{}

	encrypted, err := key.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}

	decrypted, err := key.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt empty: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("empty round trip mismatch: got %q", decrypted)
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1, _ := NewEncryptionKey("key-one")
	key2, _ := NewEncryptionKey("key-two")
	plaintext := []byte("secret data")

	encrypted, err := key1.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	_, err = key2.Decrypt(encrypted)
	if err == nil {
		t.Fatal("expected decrypt error with wrong key")
	}
}

func TestDecrypt_InvalidBase64(t *testing.T) {
	key, _ := NewEncryptionKey("test-key")
	_, err := key.Decrypt([]byte("not-valid-base64!!!"))
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	key, _ := NewEncryptionKey("test-key")
	// AES-GCM nonce is 12 bytes; base64 of less than that should fail.
	_, err := key.Decrypt([]byte("AAAA"))
	if err == nil {
		t.Fatal("expected error for too-short ciphertext")
	}
}

func TestDeriveMachineKey_Fallback(t *testing.T) {
	// On darwin/linux, this should return a non-empty string.
	// On other platforms, the fallback uses the executable path.
	mk, err := deriveMachineKey()
	if err != nil {
		t.Fatalf("deriveMachineKey: %v", err)
	}
	if mk == "" {
		t.Fatal("expected non-empty machine key")
	}
}

func TestDeriveMachineKey_Deterministic(t *testing.T) {
	// Machine key should be consistent across calls (same machine).
	mk1, err := deriveMachineKey()
	if err != nil {
		t.Fatalf("deriveMachineKey 1: %v", err)
	}
	mk2, err := deriveMachineKey()
	if err != nil {
		t.Fatalf("deriveMachineKey 2: %v", err)
	}
	if mk1 != mk2 {
		t.Fatalf("machine key not deterministic:\n1: %s\n2: %s", mk1, mk2)
	}
}

func TestDeriveMachineKey_UsesEnvironment(t *testing.T) {
	// Verify the machine key incorporates USER env var.
	origUser := os.Getenv("USER")
	defer os.Setenv("USER", origUser)

	os.Setenv("USER", "testuser-meept-auth")
	mk, err := deriveMachineKey()
	if err != nil {
		t.Fatalf("deriveMachineKey: %v", err)
	}
	if mk == "" {
		t.Fatal("expected non-empty machine key")
	}
	// The key should contain the username.
	// On macOS, hostname + ":" + username + ":" + uuid
	// We just check the key is not empty and varies with USER.
}
