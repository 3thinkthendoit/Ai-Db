package store

import (
	"context"

	"AI-DB/internal/config"
)

// FileStore is the desktop ConfigStore: a single-user JSON document (config.json
// + master.key under the OS user config dir). It ignores userID because the
// desktop build has exactly one implicit local owner. It wraps the existing
// config.Store, so the on-disk format, AES-GCM encryption and profile
// normalisation stay byte-for-byte identical to the pre-refactor behaviour.
type FileStore struct {
	inner *config.Store
}

// NewFileStore adapts an initialised config.Store to the ConfigStore interface.
func NewFileStore(inner *config.Store) *FileStore {
	return &FileStore{inner: inner}
}

// Load reads the local config document.
func (f *FileStore) Load(_ context.Context, _ string) (*config.AppConfig, error) {
	return f.inner.Load()
}

// Save writes the local config document, encrypting secrets.
func (f *FileStore) Save(_ context.Context, _ string, cfg *config.AppConfig) error {
	return f.inner.Save(cfg)
}
