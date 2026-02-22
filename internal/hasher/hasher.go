// Package hasher provides BLAKE3 file hashing.
package hasher

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/zeebo/blake3"
)

const chunkSize = 65536 // 64 KiB

// BLAKE3Hasher implements domain.Hasher using the zeebo/blake3 library.
type BLAKE3Hasher struct{}

// New returns a new BLAKE3Hasher.
func New() *BLAKE3Hasher {
	return &BLAKE3Hasher{}
}

// ComputeHash computes the BLAKE3 hex digest of the file at path.
// It respects context cancellation between read chunks.
func (h *BLAKE3Hasher) ComputeHash(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	hasher := blake3.New()
	buf := make([]byte, chunkSize)

	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}

		n, err := f.Read(buf)
		if n > 0 {
			if _, werr := hasher.Write(buf[:n]); werr != nil {
				return "", fmt.Errorf("hash write: %w", werr)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// ComputeChecksumFile is a convenience helper for hashing an arbitrary file
// (e.g., the SQLite database file itself) without context support.
func ComputeChecksumFile(path string) (string, error) {
	return (&BLAKE3Hasher{}).ComputeHash(context.Background(), path)
}
