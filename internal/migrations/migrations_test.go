// Package migrations provides migration testing for etcd_fdw database migrations.
package migrations

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrationApplication tests that migrations apply correctly
func TestMigrationApplication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping migration test in short mode")
	}

	// This would use testcontainers in integration tests
	// For now, this tests the migration logic itself

	// Test that getMigrator returns a valid migrator
	migrator, err := getMigrator()
	require.NoError(t, err, "Should create migrator instance")
	require.NotNil(t, migrator, "Should create migrator instance")

	// Test singleton behavior
	migrator2, err2 := getMigrator()
	require.NoError(t, err2, "Should create migrator instance again")
	assert.Equal(t, migrator, migrator2, "Should return same migrator instance (singleton)")
}

// TestMigrationContent tests the embedded SQL content
func TestMigrationContent(t *testing.T) {
	// Test that embedded SQL is not empty
	assert.NotEmpty(t, createTablesSQL, "Embedded SQL should not be empty")

	// Test that SQL contains expected tables
	assert.Contains(t, createTablesSQL, "CREATE TABLE etcd", "Should create etcd table")
	assert.Contains(t, createTablesSQL, "CREATE TABLE etcd_wal", "Should create etcd_wal table")

	// Test that SQL contains expected functions
	assert.Contains(t, createTablesSQL, "CREATE OR REPLACE FUNCTION etcd_get", "Should create etcd_get function")
	assert.Contains(t, createTablesSQL, "CREATE OR REPLACE FUNCTION etcd_set", "Should create etcd_set function")
	assert.Contains(t, createTablesSQL, "CREATE OR REPLACE FUNCTION etcd_delete", "Should create etcd_delete function")

	// Test that SQL contains expected triggers
	assert.Contains(t, createTablesSQL, "CREATE TRIGGER etcd_wal_notify", "Should create notify trigger")
}

// TestMigrationWithRealDatabase tests migration against a real database
func TestMigrationWithRealDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real database migration test in short mode")
	}

	// This test requires a real PostgreSQL connection
	// In practice, this would use testcontainers
	dsn := getTestDSN(t) // Function doesn't exist - will fail

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "Should connect to test database")
	defer conn.Close(ctx)

	// Apply migrations
	err = Apply(ctx, conn) // Use the Apply function instead of migrator method
	require.NoError(t, err, "Should apply migrations successfully")

	// Verify tables exist
	var tableExists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'etcd')").Scan(&tableExists)
	require.NoError(t, err, "Should check if etcd table exists")
	assert.True(t, tableExists, "etcd table should exist after migration")

	err = conn.QueryRow(ctx, "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'etcd_wal')").Scan(&tableExists)
	require.NoError(t, err, "Should check if etcd_wal table exists")
	assert.True(t, tableExists, "etcd_wal table should exist after migration")

	// Verify functions exist
	functions := []string{"etcd_get", "etcd_get_all", "etcd_set", "etcd_delete"}
	for _, funcName := range functions {
		var funcExists bool
		err = conn.QueryRow(ctx, "SELECT EXISTS (SELECT FROM pg_proc WHERE proname = $1)", funcName).Scan(&funcExists)
		require.NoError(t, err, "Should check if function %s exists", funcName)
		assert.True(t, funcExists, "Function %s should exist after migration", funcName)
	}
}

// TestMigrationFunctions tests the PostgreSQL functions created by migration
func TestMigrationFunctions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping migration functions test in short mode")
	}

	dsn := getTestDSN(t) // Function doesn't exist - will fail

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err, "Should connect to test database")
	defer conn.Close(ctx)

	// Apply migrations first
	err = Apply(ctx, conn)
	require.NoError(t, err, "Should apply migrations")

	// Test etcd_set function
	var setResult time.Time
	err = conn.QueryRow(ctx, "SELECT etcd_set($1, $2)", "test-key", "test-value").Scan(&setResult)
	require.NoError(t, err, "Should call etcd_set function")
	assert.False(t, setResult.IsZero(), "etcd_set should return valid timestamp")

	// Test etcd_get function (will return no rows initially since sync isn't implemented)
	rows, err := conn.Query(ctx, "SELECT key, value, revision, tombstone, ts FROM etcd_get($1)", "test-key")
	require.NoError(t, err, "Should call etcd_get function")
	defer rows.Close()

	// Count rows (should be 0 initially since sync isn't implemented)
	var rowCount int
	for rows.Next() {
		rowCount++
	}
	assert.Equal(t, 0, rowCount, "Should return no rows initially (sync not implemented)")

	// Test etcd_delete function
	var deleteResult time.Time
	err = conn.QueryRow(ctx, "SELECT etcd_delete($1)", "test-key").Scan(&deleteResult)
	require.NoError(t, err, "Should call etcd_delete function")
	assert.False(t, deleteResult.IsZero(), "etcd_delete should return valid timestamp")
}

// getTestDSN returns a test database connection string
// This function doesn't exist yet - will cause compilation failure (TDD)
func getTestDSN(t *testing.T) string {
	// This function is not implemented yet
	// Expected to fail during TDD phase - tests first!
	panic("getTestDSN not implemented yet - TDD approach: tests first, implementation later")
}
