// Package migrations contains database migration definitions and functionality for etcd_fdw.
package migrations

import (
	"context"
	"fmt"
	"sync"

	migrator "github.com/cybertec-postgresql/pgx-migrator"
	"github.com/jackc/pgx/v5"
)

// migrations holds function returning all upgrade migrations needed
var migrations func() migrator.Option = func() migrator.Option {
	return migrator.Migrations(
		&migrator.Migration{
			Name: "001_create_tables",
			Func: func(ctx context.Context, tx pgx.Tx) error {
				// Create all tables and indexes in a single transaction
				_, err := tx.Exec(ctx, `
					-- Main etcd table for key-value storage with revision history
					CREATE TABLE etcd (
						ts timestamp with time zone NOT NULL DEFAULT now(),
						key text NOT NULL,
						value text,
						revision bigint NOT NULL,
						tombstone boolean NOT NULL DEFAULT false,
						PRIMARY KEY(key, revision)
					);

					-- Write-ahead log table for tracking changes to be synchronized
					CREATE TABLE etcd_wal (
						id serial PRIMARY KEY,
						ts timestamp with time zone NOT NULL DEFAULT now(),
						key text NOT NULL,
						value text,
						revision bigint -- Current revision before modification (null = new key)
					);

					-- Performance indexes
					CREATE INDEX idx_etcd_key_revision ON etcd(key, revision DESC);
					CREATE INDEX idx_etcd_wal_key ON etcd_wal(key);
					CREATE INDEX idx_etcd_wal_ts ON etcd_wal(ts);
				`)
				return err
			},
		},
		// adding new migration here

		// &migrator.Migration{
		// 	Name: "Short description of a migration",
		// 	Func: func(ctx context.Context, tx pgx.Tx) error {
		// 		...
		// 	},
		// },
	)
}

var (
	migratorInstance *migrator.Migrator
	once             sync.Once
)

// getMigrator returns a singleton migrator instance
func getMigrator() (*migrator.Migrator, error) {
	var err error
	once.Do(func() {
		migratorInstance, err = migrator.New(
			migrations(),
			migrator.TableName("etcd_fdw_migrations"),
		)
	})
	return migratorInstance, err
}

// Apply applies all pending migrations to the database
func Apply(ctx context.Context, conn *pgx.Conn) error {
	m, err := getMigrator()
	if err != nil {
		return fmt.Errorf("failed to create migrator: %w", err)
	}

	// Apply migrations
	if err := m.Migrate(ctx, conn); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	return nil
}

// NeedsUpgrade checks if the database needs migration
func NeedsUpgrade(ctx context.Context, conn *pgx.Conn) (bool, error) {
	m, err := getMigrator()
	if err != nil {
		return false, fmt.Errorf("failed to create migrator: %w", err)
	}

	// Check if migration is needed
	needUpgrade, err := m.NeedUpgrade(ctx, conn)
	if err != nil {
		return false, fmt.Errorf("failed to check migration status: %w", err)
	}

	return needUpgrade, nil
}
