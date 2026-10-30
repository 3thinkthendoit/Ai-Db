package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const encPrefix = "enc:"

// Store persists app config under the user config directory.
type Store struct {
	mu   sync.Mutex
	path string
	key  []byte
}

// NewStore creates a store at the OS user config dir.
func NewStore() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return NewStoreAt(filepath.Join(dir, "aidb"))
}

// NewStoreAt creates a store under an explicit directory (used for fallback).
func NewStoreAt(base string) (*Store, error) {
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, err
	}
	key, err := loadOrCreateKey(filepath.Join(base, "master.key"))
	if err != nil {
		return nil, err
	}
	return &Store{
		path: filepath.Join(base, "config.json"),
		key:  key,
	}, nil
}

func loadOrCreateKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) == 32 {
			return data, nil
		}
		return nil, fmt.Errorf("invalid master key length")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// Load reads config from disk; missing file yields empty defaults.
func (s *Store) Load() (*AppConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := &AppConfig{Connections: []Connection{}, LLM: DefaultLLM()}
			NormalizeLLMProfiles(cfg)
			return cfg, nil
		}
		return nil, err
	}
	var cfg AppConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if cfg.Connections == nil {
		cfg.Connections = []Connection{}
	}
	for i := range cfg.Connections {
		cfg.Connections[i].Password = s.decrypt(cfg.Connections[i].Password)
		cfg.Connections[i].PasswordSet = false
	}
	cfg.LLM.APIKey = s.decrypt(cfg.LLM.APIKey)
	for i := range cfg.LLMProfiles {
		cfg.LLMProfiles[i].APIKey = s.decrypt(cfg.LLMProfiles[i].APIKey)
	}
	NormalizeLLMProfiles(&cfg)
	return &cfg, nil
}

// Save writes config to disk, encrypting secrets.
func (s *Store) Save(cfg *AppConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	NormalizeLLMProfiles(cfg)

	clone := *cfg
	clone.Connections = make([]Connection, len(cfg.Connections))
	copy(clone.Connections, cfg.Connections)
	for i := range clone.Connections {
		enc, err := s.encrypt(clone.Connections[i].Password)
		if err != nil {
			return err
		}
		clone.Connections[i].Password = enc
		clone.Connections[i].PasswordSet = false
	}
	clone.LLMProfiles = make([]LLMProfile, len(cfg.LLMProfiles))
	copy(clone.LLMProfiles, cfg.LLMProfiles)
	for i := range clone.LLMProfiles {
		enc, err := s.encrypt(clone.LLMProfiles[i].APIKey)
		if err != nil {
			return err
		}
		clone.LLMProfiles[i].APIKey = enc
	}
	encKey, err := s.encrypt(clone.LLM.APIKey)
	if err != nil {
		return err
	}
	clone.LLM.APIKey = encKey

	data, err := json.MarshalIndent(clone, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if len(plain) >= len(encPrefix) && plain[:len(encPrefix)] == encPrefix {
		return plain, nil
	}
	block, err := aes.NewCipher(s.key)
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

func (s *Store) decrypt(value string) string {
	if value == "" || len(value) < len(encPrefix) || value[:len(encPrefix)] != encPrefix {
		return value
	}
	raw, err := base64.StdEncoding.DecodeString(value[len(encPrefix):])
	if err != nil {
		return ""
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	if len(raw) < gcm.NonceSize() {
		return ""
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

// MaskedLLM returns LLM config with API key masked for UI display.
func MaskedLLM(llm LLMConfig) LLMConfig {
	out := llm
	if out.APIKey != "" {
		out.APIKey = fmt.Sprintf("••••%s", last4(out.APIKey))
	}
	return out
}

func last4(s string) string {
	if len(s) <= 4 {
		return s
	}
	return s[len(s)-4:]
}
