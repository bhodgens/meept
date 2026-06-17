// Package auth provides OAuth device code flow and encrypted token storage
// for LLM providers that support device authorization (RFC 8628).
package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
)

const aesKeySize = 32 // AES-256

// EncryptionKey provides AES-256-GCM encryption for token storage.
// The key is resolved at construction time from a user-provided key or
// machine-derived identifiers.
type EncryptionKey struct {
	key []byte
}

// NewEncryptionKey creates an encryption key. If userKey is non-empty, it is
// used directly (after hashing to 32 bytes). Otherwise a machine-derived key is
// generated from platform-specific identifiers.
func NewEncryptionKey(userKey string) (*EncryptionKey, error) {
	var raw string
	if userKey != "" {
		raw = userKey
	} else {
		mk, err := deriveMachineKey()
		if err != nil {
			return nil, fmt.Errorf("derive machine key: %w", err)
		}
		raw = mk
	}
	hash := sha256.Sum256([]byte(raw))
	return &EncryptionKey{key: hash[:]}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM.
// The returned slice contains base64(nonce || ciphertext+tag).
func (k *EncryptionKey) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(k.key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	// Seal appends ciphertext+tag to nonce.
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(ciphertext)))
	base64.StdEncoding.Encode(encoded, ciphertext)
	return encoded, nil
}

// Decrypt decrypts data produced by Encrypt.
// Input format: base64(nonce || ciphertext+tag).
func (k *EncryptionKey) Decrypt(encoded []byte) ([]byte, error) {
	ciphertext := make([]byte, base64.StdEncoding.DecodedLen(len(encoded)))
	n, err := base64.StdEncoding.Decode(ciphertext, encoded)
	if err != nil {
		return nil, fmt.Errorf("base64 decode: %w", err)
	}
	ciphertext = ciphertext[:n]

	block, err := aes.NewCipher(k.key)
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

// Key returns the raw 32-byte key. Used for testing key derivation consistency.
func (k *EncryptionKey) Key() []byte {
	cp := make([]byte, len(k.key))
	copy(cp, k.key)
	return cp
}

// deriveMachineKey generates a 256-bit key from machine-specific identifiers.
// It combines hostname + username + a platform-specific hardware ID.
//
// Stability note: the hostname component can change across reboots, DHCP
// renewals, or container rebuilds, which invalidates previously-encrypted
// tokens. Operators who need stable cross-environment keys should pass an
// explicit userKey to NewEncryptionKey, which bypasses this derivation
// entirely and uses the operator-supplied value instead.
//
// The platformMachineID function is defined in per-platform files (darwin, linux, other).
func deriveMachineKey() (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}
	if username == "" {
		username = "unknown"
	}

	hwID, err := platformMachineID()
	if err != nil {
		return "", fmt.Errorf("hardware id: %w", err)
	}
	return hostname + ":" + username + ":" + hwID, nil
}
