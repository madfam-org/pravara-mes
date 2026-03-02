// Package db provides database connection and repository implementations.
package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
)

// DB wraps the database connection with additional functionality.
type DB struct {
	*sql.DB
}

// NewConnection creates a new database connection.
func NewConnection(cfg config.DatabaseConfig) (*DB, error) {
	db, err := sql.Open("postgres", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxConnections)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)

	// Verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{db}, nil
}

// SetTenantID sets the current tenant ID for Row-Level Security.
// This should be called at the beginning of each request.
func (db *DB) SetTenantID(tenantID string) error {
	_, err := db.Exec(fmt.Sprintf("SET app.current_tenant_id = '%s'", tenantID))
	return err
}

// ClearTenantID clears the current tenant ID setting.
func (db *DB) ClearTenantID() error {
	_, err := db.Exec("RESET app.current_tenant_id")
	return err
}

// Health checks database connectivity.
func (db *DB) Health() error {
	return db.Ping()
}

// Stats returns database connection pool statistics.
func (db *DB) Stats() sql.DBStats {
	return db.DB.Stats()
}
