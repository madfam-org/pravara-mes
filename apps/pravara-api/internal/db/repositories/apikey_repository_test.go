package repositories

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyRepository_Create(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewAPIKeyRepository(db)

	tests := []struct {
		name      string
		key       *APIKey
		mockSetup func(sqlmock.Sqlmock)
		wantError bool
	}{
		{
			name: "create key successfully",
			key: &APIKey{
				TenantID:  uuid.New(),
				Name:      "Production API Key",
				KeyHash:   "sha256_abc123",
				KeyPrefix: "pk_live_",
				Scopes:    []string{"read:orders", "write:orders"},
				RateLimit: 1000,
				IsActive:  true,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())
				mock.ExpectQuery("INSERT INTO api_keys").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create key with nil ID generates new ID",
			key: &APIKey{
				ID:        uuid.Nil,
				TenantID:  uuid.New(),
				Name:      "Test Key",
				KeyHash:   "sha256_def456",
				KeyPrefix: "pk_test_",
				Scopes:    []string{"read:machines"},
				RateLimit: 100,
				IsActive:  true,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())
				mock.ExpectQuery("INSERT INTO api_keys").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create key with expiration and creator",
			key: &APIKey{
				TenantID:  uuid.New(),
				Name:      "Temporary Key",
				KeyHash:   "sha256_ghi789",
				KeyPrefix: "pk_tmp_",
				Scopes:    []string{"read:orders"},
				RateLimit: 50,
				IsActive:  true,
				ExpiresAt: func() *time.Time { t := time.Now().Add(24 * time.Hour); return &t }(),
				CreatedBy: func() *uuid.UUID { u := uuid.New(); return &u }(),
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{"created_at", "updated_at"}).
					AddRow(time.Now(), time.Now())
				mock.ExpectQuery("INSERT INTO api_keys").
					WillReturnRows(rows)
			},
			wantError: false,
		},
		{
			name: "create key database error",
			key: &APIKey{
				TenantID:  uuid.New(),
				Name:      "Failing Key",
				KeyHash:   "sha256_fail",
				KeyPrefix: "pk_fail_",
				Scopes:    []string{"read:orders"},
				RateLimit: 100,
				IsActive:  true,
			},
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("INSERT INTO api_keys").
					WillReturnError(sql.ErrConnDone)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalID := tt.key.ID
			tt.mockSetup(mock)

			err := repo.Create(context.Background(), tt.key)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if originalID == uuid.Nil {
					assert.NotEqual(t, uuid.Nil, tt.key.ID, "ID should be generated when nil")
				}
				assert.False(t, tt.key.CreatedAt.IsZero())
				assert.False(t, tt.key.UpdatedAt.IsZero())
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAPIKeyRepository_GetByHash(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewAPIKeyRepository(db)

	tests := []struct {
		name      string
		keyHash   string
		mockSetup func(sqlmock.Sqlmock)
		wantKey   bool
		wantError bool
	}{
		{
			name:    "find key by hash",
			keyHash: "sha256_abc123",
			mockSetup: func(mock sqlmock.Sqlmock) {
				keyID := uuid.New()
				tenantID := uuid.New()
				now := time.Now()

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "key_hash", "key_prefix", "scopes", "rate_limit",
					"is_active", "expires_at", "last_used_at", "created_by", "created_at", "updated_at",
				}).AddRow(
					keyID, tenantID, "Prod Key", "sha256_abc123", "pk_live_",
					pq.Array([]string{"read:orders", "write:orders"}), 1000,
					true, nil, nil, nil, now, now,
				)

				mock.ExpectQuery("SELECT id, tenant_id.*FROM api_keys WHERE key_hash").
					WithArgs("sha256_abc123").
					WillReturnRows(rows)
			},
			wantKey:   true,
			wantError: false,
		},
		{
			name:    "find key by hash with optional fields populated",
			keyHash: "sha256_full",
			mockSetup: func(mock sqlmock.Sqlmock) {
				keyID := uuid.New()
				tenantID := uuid.New()
				creatorID := uuid.New()
				now := time.Now()
				expiresAt := now.Add(24 * time.Hour)

				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "key_hash", "key_prefix", "scopes", "rate_limit",
					"is_active", "expires_at", "last_used_at", "created_by", "created_at", "updated_at",
				}).AddRow(
					keyID, tenantID, "Full Key", "sha256_full", "pk_live_",
					pq.Array([]string{"read:all"}), 500,
					true, expiresAt, now, creatorID.String(), now, now,
				)

				mock.ExpectQuery("SELECT id, tenant_id.*FROM api_keys WHERE key_hash").
					WithArgs("sha256_full").
					WillReturnRows(rows)
			},
			wantKey:   true,
			wantError: false,
		},
		{
			name:    "key not found",
			keyHash: "sha256_notfound",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT id, tenant_id.*FROM api_keys WHERE key_hash").
					WithArgs("sha256_notfound").
					WillReturnError(sql.ErrNoRows)
			},
			wantKey:   false,
			wantError: false,
		},
		{
			name:    "database error",
			keyHash: "sha256_error",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT id, tenant_id.*FROM api_keys WHERE key_hash").
					WithArgs("sha256_error").
					WillReturnError(sql.ErrConnDone)
			},
			wantKey:   false,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			key, err := repo.GetByHash(context.Background(), tt.keyHash)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if tt.wantKey {
					require.NotNil(t, key)
					assert.Equal(t, tt.keyHash, key.KeyHash)
					assert.True(t, key.IsActive)
				} else {
					assert.Nil(t, key)
				}
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAPIKeyRepository_List(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewAPIKeyRepository(db)

	tests := []struct {
		name      string
		mockSetup func(sqlmock.Sqlmock)
		wantCount int
		wantError bool
	}{
		{
			name: "list keys for tenant",
			mockSetup: func(mock sqlmock.Sqlmock) {
				now := time.Now()
				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "key_prefix", "scopes", "rate_limit",
					"is_active", "expires_at", "last_used_at", "created_by", "created_at", "updated_at",
				}).
					AddRow(uuid.New(), uuid.New(), "Key 1", "pk_live_", pq.Array([]string{"read:all"}),
						1000, true, nil, nil, nil, now, now).
					AddRow(uuid.New(), uuid.New(), "Key 2", "pk_test_", pq.Array([]string{"read:orders"}),
						100, true, nil, now, nil, now, now)

				mock.ExpectQuery("SELECT id, tenant_id.*FROM api_keys.*ORDER BY created_at DESC").
					WillReturnRows(rows)
			},
			wantCount: 2,
			wantError: false,
		},
		{
			name: "empty list",
			mockSetup: func(mock sqlmock.Sqlmock) {
				rows := sqlmock.NewRows([]string{
					"id", "tenant_id", "name", "key_prefix", "scopes", "rate_limit",
					"is_active", "expires_at", "last_used_at", "created_by", "created_at", "updated_at",
				})
				mock.ExpectQuery("SELECT id, tenant_id.*FROM api_keys.*ORDER BY created_at DESC").
					WillReturnRows(rows)
			},
			wantCount: 0,
			wantError: false,
		},
		{
			name: "database error",
			mockSetup: func(mock sqlmock.Sqlmock) {
				mock.ExpectQuery("SELECT id, tenant_id.*FROM api_keys.*ORDER BY created_at DESC").
					WillReturnError(sql.ErrConnDone)
			},
			wantCount: 0,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock)

			keys, err := repo.List(context.Background())

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Len(t, keys, tt.wantCount)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAPIKeyRepository_Revoke(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewAPIKeyRepository(db)

	tests := []struct {
		name      string
		keyID     uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantError bool
		wantErr   error
	}{
		{
			name:  "revoke key successfully",
			keyID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE api_keys SET is_active = FALSE WHERE id").
					WithArgs(id).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:  "revoke key not found",
			keyID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE api_keys SET is_active = FALSE WHERE id").
					WithArgs(id).
					WillReturnResult(sqlmock.NewResult(0, 0))
			},
			wantError: true,
			wantErr:   ErrNotFound,
		},
		{
			name:  "revoke key database error",
			keyID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE api_keys SET is_active = FALSE WHERE id").
					WithArgs(id).
					WillReturnError(sql.ErrConnDone)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.keyID)

			err := repo.Revoke(context.Background(), tt.keyID)

			if tt.wantError {
				assert.Error(t, err)
				if tt.wantErr != nil {
					assert.Equal(t, tt.wantErr, err)
				}
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAPIKeyRepository_UpdateLastUsed(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewAPIKeyRepository(db)

	tests := []struct {
		name      string
		keyID     uuid.UUID
		mockSetup func(sqlmock.Sqlmock, uuid.UUID)
		wantError bool
	}{
		{
			name:  "update last used successfully",
			keyID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE api_keys SET last_used_at").
					WithArgs(id).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
			wantError: false,
		},
		{
			name:  "update last used database error",
			keyID: uuid.New(),
			mockSetup: func(mock sqlmock.Sqlmock, id uuid.UUID) {
				mock.ExpectExec("UPDATE api_keys SET last_used_at").
					WithArgs(id).
					WillReturnError(sql.ErrConnDone)
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.mockSetup(mock, tt.keyID)

			err := repo.UpdateLastUsed(context.Background(), tt.keyID)

			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestNewAPIKeyRepository(t *testing.T) {
	db, _, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()

	repo := NewAPIKeyRepository(db)
	assert.NotNil(t, repo)
}

func TestAPIKey_Structure(t *testing.T) {
	id := uuid.New()
	tenantID := uuid.New()
	createdBy := uuid.New()
	now := time.Now()
	expiresAt := now.Add(30 * 24 * time.Hour)

	key := APIKey{
		ID:         id,
		TenantID:   tenantID,
		Name:       "Production Key",
		KeyHash:    "sha256_hash",
		KeyPrefix:  "pk_live_abc",
		Scopes:     []string{"read:orders", "write:orders", "read:machines"},
		RateLimit:  1000,
		IsActive:   true,
		ExpiresAt:  &expiresAt,
		LastUsedAt: &now,
		CreatedBy:  &createdBy,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	assert.Equal(t, id, key.ID)
	assert.Equal(t, tenantID, key.TenantID)
	assert.Equal(t, "Production Key", key.Name)
	assert.Equal(t, "sha256_hash", key.KeyHash)
	assert.Equal(t, "pk_live_abc", key.KeyPrefix)
	assert.Len(t, key.Scopes, 3)
	assert.Equal(t, 1000, key.RateLimit)
	assert.True(t, key.IsActive)
	assert.NotNil(t, key.ExpiresAt)
	assert.NotNil(t, key.LastUsedAt)
	assert.NotNil(t, key.CreatedBy)
}
