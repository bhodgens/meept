package resources

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"

	"github.com/zeebo/blake3"
)

// newBlake3Hasher returns a blake3 hasher conforming to hash.Hash.
func newBlake3Hasher() hash.Hash {
	return blake3.New()
}

// HashFile computes the hash of a file using the specified algorithm.
// Supported algorithms: "blake3" (default), "sha256".
// Returns the hex-encoded hash.
func HashFile(path, algo string) (string, error) {
	switch algo {
	case AlgoBlake3, "":
		return hashFile(path, newBlake3Hasher)
	case AlgoSHA256:
		return hashFile(path, func() hash.Hash { return sha256.New() })
	default:
		return "", errors.New("resources: unsupported hash algorithm: " + algo)
	}
}

// HashBytes computes the hash of a byte slice using the specified algorithm.
func HashBytes(data []byte, algo string) (string, error) {
	switch algo {
	case AlgoBlake3, "":
		h := newBlake3Hasher()
		h.Write(data)
		return hex.EncodeToString(h.Sum(nil)), nil
	case AlgoSHA256:
		h := sha256.New()
		h.Write(data)
		return hex.EncodeToString(h.Sum(nil)), nil
	default:
		return "", errors.New("resources: unsupported hash algorithm: " + algo)
	}
}

// hashFile is the shared streaming-hash implementation. It reads the file
// in chunks to avoid loading large files (models, datasets) into memory.
// The hasher factory avoids a hard import dependency at the call site.
func hashFile(path string, newHasher func() hash.Hash) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := newHasher()
	buf := make([]byte, 64*1024) // 64 KB read buffer
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
