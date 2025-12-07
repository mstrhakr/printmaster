package storage

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id password hashing parameters
const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// hashArgon generates an Argon2id hash for the given secret.
// Returns the encoded hash in the format: $argon2id$v=19$m=...,t=...,p=...$<salt_b64>$<hash_b64>
func hashArgon(secret string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(secret), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)
	encoded := fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, b64Salt, b64Hash)

	return encoded, nil
}

// verifyArgonHash verifies a secret against an Argon2id encoded hash.
// Returns true if the secret matches, false otherwise.
func verifyArgonHash(secret, encoded string) (bool, error) {
	// encoded format: $argon2id$v=19$m=<mem>,t=<time>,p=<threads>$<salt>$<hash>
	parts := strings.Split(encoded, "$")
	if len(parts) < 6 {
		return false, fmt.Errorf("bad encoded hash format")
	}

	params := parts[3]
	saltB64 := parts[4]
	hashB64 := parts[5]

	// Parse parameters
	var memory, time uint32
	var threads uint8

	// Try standard format first
	_, err := fmt.Sscanf(params, "m=%d,t=%d,p=%d", &memory, &time, &threads)
	if err != nil {
		// Fallback: parse comma-separated key=value pairs
		vals := strings.Split(params, ",")
		for _, v := range vals {
			kv := strings.SplitN(v, "=", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "m":
				fmt.Sscanf(kv[1], "%d", &memory)
			case "t":
				fmt.Sscanf(kv[1], "%d", &time)
			case "p":
				fmt.Sscanf(kv[1], "%d", &threads)
			}
		}
	}

	salt, err := base64.RawStdEncoding.DecodeString(saltB64)
	if err != nil {
		return false, fmt.Errorf("failed to decode salt: %w", err)
	}

	expected, err := base64.RawStdEncoding.DecodeString(hashB64)
	if err != nil {
		return false, fmt.Errorf("failed to decode hash: %w", err)
	}

	keyLen := uint32(len(expected))
	derived := argon2.IDKey([]byte(secret), salt, time, memory, threads, keyLen)

	return subtleConstantTimeCompare(derived, expected), nil
}

// subtleConstantTimeCompare performs a constant-time comparison of two byte slices
// to prevent timing attacks.
func subtleConstantTimeCompare(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// boolToInt converts a boolean to an integer (0 or 1) for database storage.
// Note: Both SQLite and PostgreSQL drivers can handle bool directly, so this
// is kept for backwards compatibility but not strictly necessary.
func boolToInt(b bool) interface{} {
	return b // Return bool directly - both drivers handle it correctly
}

// intToBool converts a database value (int, int64, or bool) to a boolean.
func intToBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	default:
		return false
	}
}
