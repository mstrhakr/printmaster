package packager

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"printmaster/common/util"
)

type bundleEncryptor struct {
	key   []byte
	keyID string
}

func newBundleEncryptor(path string) (*bundleEncryptor, error) {
	if path == "" {
		return nil, fmt.Errorf("encryption key path required")
	}
	key, err := util.LoadOrCreateKey(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(key)
	return &bundleEncryptor{
		key:   key,
		keyID: hex.EncodeToString(sum[:8]),
	}, nil
}

func (e *bundleEncryptor) keyIdentifier() string {
	if e == nil {
		return ""
	}
	return e.keyID
}

func (e *bundleEncryptor) encryptFileInPlace(path string) (int64, error) {
	if e == nil {
		return 0, fmt.Errorf("encryptor not configured")
	}
	plaintext, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	sealed, err := util.EncryptBytes(e.key, plaintext)
	zeroBytes(plaintext)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(path, sealed, 0o600); err != nil {
		return 0, err
	}
	return int64(len(plaintext)), nil
}

func (e *bundleEncryptor) decryptFile(path string) ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("encryptor not configured")
	}
	sealed, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return util.DecryptBytes(e.key, sealed)
}

func zeroBytes(buf []byte) {
	for i := range buf {
		buf[i] = 0
	}
}
