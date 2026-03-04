package repositories

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// APIKey represents an API key record.
type APIKey struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	Name       string     `json:"name"`
	KeyHash    string     `json:"-"`
	KeyPrefix  string     `json:"key_prefix"`
	Scopes     []string   `json:"scopes"`
	RateLimit  int        `json:"rate_limit"`
	IsActive   bool       `json:"is_active"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedBy  *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// APIKeyRepository handles API key database operations.
type APIKeyRepository struct {
	db *sql.DB
}

// NewAPIKeyRepository creates a new API key repository.
func NewAPIKeyRepository(db *sql.DB) *APIKeyRepository {
	return &APIKeyRepository{db: db}
}

// Create inserts a new API key.
func (r *APIKeyRepository) Create(ctx context.Context, key *APIKey) error {
	if key.ID == uuid.Nil {
		key.ID = uuid.New()
	}
	err := r.db.QueryRowContext(ctx,
		`INSERT INTO api_keys (id, tenant_id, name, key_hash, key_prefix, scopes, rate_limit, is_active, expires_at, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING created_at, updated_at`,
		key.ID, key.TenantID, key.Name, key.KeyHash, key.KeyPrefix,
		pq.Array(key.Scopes), key.RateLimit, key.IsActive, key.ExpiresAt, key.CreatedBy,
	).Scan(&key.CreatedAt, &key.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create API key: %w", err)
	}
	return nil
}

// GetByHash looks up an API key by its SHA-256 hash. This bypasses RLS for auth lookup.
func (r *APIKeyRepository) GetByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	var key APIKey
	var expiresAt sql.NullTime
	var lastUsedAt sql.NullTime
	var createdBy sql.NullString

	err := r.db.QueryRowContext(ctx,
		`SELECT id, tenant_id, name, key_hash, key_prefix, scopes, rate_limit,
		        is_active, expires_at, last_used_at, created_by, created_at, updated_at
		 FROM api_keys WHERE key_hash = $1`,
		keyHash,
	).Scan(&key.ID, &key.TenantID, &key.Name, &key.KeyHash, &key.KeyPrefix,
		pq.Array(&key.Scopes), &key.RateLimit, &key.IsActive,
		&expiresAt, &lastUsedAt, &createdBy, &key.CreatedAt, &key.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get API key by hash: %w", err)
	}

	if expiresAt.Valid {
		key.ExpiresAt = &expiresAt.Time
	}
	if lastUsedAt.Valid {
		key.LastUsedAt = &lastUsedAt.Time
	}
	if createdBy.Valid {
		id, _ := uuid.Parse(createdBy.String)
		key.CreatedBy = &id
	}

	return &key, nil
}

// List returns all API keys for the current tenant (via RLS).
func (r *APIKeyRepository) List(ctx context.Context) ([]APIKey, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, tenant_id, name, key_prefix, scopes, rate_limit,
		        is_active, expires_at, last_used_at, created_by, created_at, updated_at
		 FROM api_keys
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list API keys: %w", err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var key APIKey
		var expiresAt sql.NullTime
		var lastUsedAt sql.NullTime
		var createdBy sql.NullString

		if err := rows.Scan(&key.ID, &key.TenantID, &key.Name, &key.KeyPrefix,
			pq.Array(&key.Scopes), &key.RateLimit, &key.IsActive,
			&expiresAt, &lastUsedAt, &createdBy, &key.CreatedAt, &key.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan API key: %w", err)
		}

		if expiresAt.Valid {
			key.ExpiresAt = &expiresAt.Time
		}
		if lastUsedAt.Valid {
			key.LastUsedAt = &lastUsedAt.Time
		}
		if createdBy.Valid {
			id, _ := uuid.Parse(createdBy.String)
			key.CreatedBy = &id
		}

		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// Revoke deactivates an API key.
func (r *APIKeyRepository) Revoke(ctx context.Context, id uuid.UUID) error {
	result, err := r.db.ExecContext(ctx,
		`UPDATE api_keys SET is_active = FALSE WHERE id = $1`,
		id,
	)
	if err != nil {
		return fmt.Errorf("failed to revoke API key: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateLastUsed updates the last_used_at timestamp for an API key.
func (r *APIKeyRepository) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`,
		id,
	)
	return err
}
