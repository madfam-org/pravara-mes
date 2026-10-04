// Package db provides database connection and repository implementations.
package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/sirupsen/logrus"

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

// Tenant returns a handle that runs every statement inside the tenant or
// system scope carried by the statement's context (see tenant_scope.go).
// Repositories are built on it; a statement without a scope fails.
func (db *DB) Tenant(log *logrus.Logger) *TenantDB {
	return NewTenantDB(db.DB, log)
}

// Health checks database connectivity.
func (db *DB) Health() error {
	return db.Ping()
}

// Stats returns database connection pool statistics.
func (db *DB) Stats() sql.DBStats {
	return db.DB.Stats()
}
