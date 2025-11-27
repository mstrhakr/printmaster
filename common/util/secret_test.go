package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptBytesRoundTrip(t *testing.T) {
	t.Parallel()

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plaintext := []byte("hello secure world")
	sealed, err := EncryptBytes(key, plaintext)
	if err != nil {
		t.Fatalf("EncryptBytes failed: %v", err)
	}
	if string(sealed[:len(plaintext)]) == string(plaintext) {
		t.Fatalf("ciphertext appears unencrypted")
	}
	recovered, err := DecryptBytes(key, sealed)
	if err != nil {
		t.Fatalf("DecryptBytes failed: %v", err)
	}
	if string(recovered) != string(plaintext) {
		t.Fatalf("expected %q, got %q", plaintext, recovered)
	}
}

func TestLoadOrCreateKeyInvalidLength(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "bad.key")
	if err := os.WriteFile(keyPath, []byte("short"), 0o600); err != nil {
		t.Fatalf("failed to write temp key: %v", err)
	}
	if _, err := LoadOrCreateKey(keyPath); err == nil {
		t.Fatalf("expected error for invalid key length")
	}
	// Remove bad key and ensure new one is generated
	if err := os.Remove(keyPath); err != nil {
		t.Fatalf("failed to remove temp key: %v", err)
	}
	key, err := LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateKey failed: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}
}
