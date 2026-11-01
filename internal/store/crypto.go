package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

// encPrefix marks a value as AES-256-GCM ciphertext (base64 of nonce||seal).
// It matches the desktop config store's scheme so ciphertexts stay portable
// between the file and MySQL stores.
const encPrefix = "enc:"

// keyLen is the required AES-256 key size in bytes.
const keyLen = 32

// Cipher encrypts and decrypts secrets at rest.
//
// NOTE: internal/config/store.go currently carries an identical copy of this
// logic for the desktop build. Phase 1 unifies them by making the desktop store
// delegate here; Phase 0 keeps this standalone so the desktop path is untouched.
type Cipher struct {
	key []byte
}

// NewCipher validates the key length and returns a ready cipher.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("invalid master key length %d (want %d)", len(key), keyLen)
	}
	return &Cipher{key: key}, nil
}

// LoadOrCreateKey reads a 32-byte key file, creating it with fresh random bytes
// if it does not yet exist. Used for the server master key and session secret.
func LoadOrCreateKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) == keyLen {
			return data, nil
		}
		return nil, fmt.Errorf("invalid master key length")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, keyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// Encrypt seals a plaintext secret. Empty strings and already-encrypted values
// pass through unchanged so callers can re-save without double-encrypting.
func (c *Cipher) Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if len(plain) >= len(encPrefix) && plain[:len(encPrefix)] == encPrefix {
		return plain, nil
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt opens a ciphertext. Values without the enc: marker are treated as
// plaintext and returned as-is. A marked value that cannot be decoded or fails
// GCM authentication (wrong/rotated master key, corrupt storage) returns an error
// rather than an empty string, so a caller never mistakes a decryption failure
// for "no secret" and then overwrites good ciphertext with empty on the next
// save.
func (c *Cipher) Decrypt(value string) (string, error) {
	if value == "" || len(value) < len(encPrefix) || value[:len(encPrefix)] != encPrefix {
		return value, nil
	}
	raw, err := base64.StdEncoding.DecodeString(value[len(encPrefix):])
	if err != nil {
		return "", fmt.Errorf("decrypt: malformed ciphertext: %w", err)
	}
	block, err := aes.NewCipher(c.key)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("decrypt: ciphertext too short")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plain), nil
}
