package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
)

// CompressFile compresses src to dst.zst using zstd and returns the compressed size.
func CompressFile(src, dst string) (compressedSize int64, err error) {
	// Read source file
	srcData, err := os.ReadFile(src)
	if err != nil {
		return 0, Wrap("compress_read", err)
	}

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return 0, Wrap("compress_mkdir", err)
	}

	// Compress with zstd (default level for speed)
	dstFile, err := os.Create(dst + ".zst")
	if err != nil {
		return 0, Wrap("compress_write", err)
	}
	defer dstFile.Close()

	writer, err := zstd.NewWriter(dstFile, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return 0, Wrap("compress_zstd", err)
	}
	defer writer.Close()

	n, err := writer.Write(srcData)
	if err != nil {
		return int64(n), Wrap("compress_write_data", err)
	}

	if err := writer.Close(); err != nil {
		return int64(n), Wrap("compress_flush", err)
	}

	if err := dstFile.Close(); err != nil {
		return int64(n), Wrap("compress_close", err)
	}

	return int64(n), nil
}

// DecompressFile decompresses src.zst to dst.
func DecompressFile(src, dst string) error {
	// Open compressed file
	srcFile, err := os.Open(src)
	if err != nil {
		return Wrap("decompress_read", err)
	}
	defer srcFile.Close()

	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return Wrap("decompress_mkdir", err)
	}

	dstFile, err := os.Create(dst)
	if err != nil {
		return Wrap("decompress_write", err)
	}
	defer dstFile.Close()

	reader, err := zstd.NewReader(srcFile)
	if err != nil {
		return Wrap("decompress_zstd", err)
	}
	defer reader.Close()

	if _, err := io.Copy(dstFile, reader); err != nil {
		return Wrap("decompress_copy", err)
	}

	if err := dstFile.Close(); err != nil {
		return Wrap("decompress_close", err)
	}

	return nil
}

// ComputeSHA256 returns the hex-encoded SHA256 hash of a file.
func ComputeSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", Wrap("sha256_read", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", Wrap("sha256_hash", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// CompressData compresses a byte slice using zstd and returns the result.
func CompressData(data []byte) ([]byte, error) {
	var buf strings.Builder
	writer, err := zstd.NewWriter(&buf)
	if err != nil {
		return nil, Wrap("compress_data_zstd", err)
	}
	defer writer.Close()

	if _, err := writer.Write(data); err != nil {
		return nil, Wrap("compress_data_write", err)
	}

	if err := writer.Close(); err != nil {
		return nil, Wrap("compress_data_close", err)
	}

	return []byte(buf.String()), nil
}

// DecompressData decompresses a zstd-compressed byte slice.
func DecompressData(compressed []byte) ([]byte, error) {
	reader, err := zstd.NewReader(nil)
	if err != nil {
		return nil, Wrap("decompress_data_zstd", err)
	}
	defer reader.Close()

	return reader.DecodeAll(compressed, nil)
}
