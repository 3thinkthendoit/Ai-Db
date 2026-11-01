// Package store abstracts config persistence behind an owner-scoped interface so
// the desktop build (single local user, JSON file) and the server build
// (multi-user, MySQL) can share the same application service layer.
package store

import (
	"context"

	"AI-DB/internal/config"
)

// ConfigStore persists a user's whole config document. Every method is
// owner-scoped: implementations MUST partition by userID so one tenant can never
// read or mutate another tenant's data.
//
// The document-granularity Load/Save shape (rather than fine-grained CRUD) lets
// the shared app.Service reuse the desktop mutation logic verbatim — the same
// in-memory AppConfig edits plus the NormalizeLLMProfiles/SyncActiveLLM coupling
// — which is what guarantees the desktop build's behaviour stays unchanged.
type ConfigStore interface {
	// Load returns the user's config. A brand-new user yields normalised
	// defaults (empty connections, DefaultLLM); implementations should reserve
	// errors for genuine I/O failures.
	Load(ctx context.Context, userID string) (*config.AppConfig, error)
	// Save persists the user's config, encrypting secrets at rest.
	Save(ctx context.Context, userID string, cfg *config.AppConfig) error
}
