// Package migrations provides migration testing for etcd_fdw database migrations.
package migrations

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
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

	// Test that SQL contains expected functions for single table architecture
	assert.Contains(t, createTablesSQL, "CREATE OR REPLACE FUNCTION etcd_get", "Should create etcd_get function")
	assert.Contains(t, createTablesSQL, "CREATE OR REPLACE FUNCTION etcd_put", "Should create etcd_put function")
	assert.Contains(t, createTablesSQL, "CREATE OR REPLACE FUNCTION etcd_delete", "Should create etcd_delete function")
	assert.Contains(t, createTablesSQL, "CREATE OR REPLACE FUNCTION etcd_get_pending", "Should create etcd_get_pending function")
	assert.Contains(t, createTablesSQL, "CREATE OR REPLACE FUNCTION etcd_update_revision", "Should create etcd_update_revision function")

	// Test that SQL contains expected indexes
	assert.Contains(t, createTablesSQL, "CREATE INDEX idx_etcd_pending", "Should create pending index")

	// Test revision encoding comments
	assert.Contains(t, createTablesSQL, "revision = -1", "Should document revision encoding")
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

	// Verify functions exist (updated for single table architecture)
	functions := []string{"etcd_get", "etcd_get_all", "etcd_put", "etcd_delete", "etcd_get_pending", "etcd_update_revision"}
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

	// Test etcd_put function (updated from etcd_set)
	var putResult time.Time
	err = conn.QueryRow(ctx, "SELECT etcd_put($1, $2)", "test-key", "test-value").Scan(&putResult)
	require.NoError(t, err, "Should call etcd_put function")
	assert.False(t, putResult.IsZero(), "etcd_put should return valid timestamp")

	// Test etcd_get function (returns latest record including pending ones)
	rows, err := conn.Query(ctx, "SELECT key, value, revision, tombstone, ts FROM etcd_get($1)", "test-key")
	require.NoError(t, err, "Should call etcd_get function")
	defer rows.Close()

	// Count rows (should be 1 since etcd_get returns latest record including pending ones)
	var rowCount int
	for rows.Next() {
		rowCount++
	}
	assert.Equal(t, 1, rowCount, "Should return 1 record (the pending record created by etcd_put)")

	// Test etcd_get_pending function (should find the pending record)
	pendingRows, err := conn.Query(ctx, "SELECT * FROM etcd_get_pending()")
	require.NoError(t, err, "Should call etcd_get_pending function")
	defer pendingRows.Close()

	var pendingCount int
	for pendingRows.Next() {
		pendingCount++
	}
	assert.Equal(t, 1, pendingCount, "Should return 1 pending record after etcd_put")

	// Test etcd_delete function with different key to avoid primary key conflict
	var deleteResult time.Time
	err = conn.QueryRow(ctx, "SELECT etcd_delete($1)", "test-key-delete").Scan(&deleteResult)
	require.NoError(t, err, "Should call etcd_delete function")
	assert.False(t, deleteResult.IsZero(), "etcd_delete should return valid timestamp")
}

// getTestDSN returns a test database connection string
func getTestDSN(t *testing.T) string {
	// Use testcontainers for real database testing
	ctx := context.Background()

	// Start PostgreSQL container with proper wait strategy
	pgContainer, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second)),
	)
	require.NoError(t, err)

	// Cleanup container when test ends
	t.Cleanup(func() {
		pgContainer.Terminate(ctx)
	})

	// Get connection string
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	return connStr
}
