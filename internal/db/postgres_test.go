// Package db provides PostgreSQL database testing for etcd_fdw.
package db

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewPostgreSQLClient tests PostgreSQL client creation
func TestNewPostgreSQLClient(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// This is an integration test that requires a real PostgreSQL instance
	pool, err := New(ctx, "")
	if err != nil {
		t.Skipf("PostgreSQL not available for testing: %v", err)
	}
	defer pool.Close()

	// Test basic functionality
	assert.NotNil(t, pool, "Pool should not be nil")
}

// TestBulkInsert tests bulk insert functionality with mock
func TestBulkInsert(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	// Set up mock expectations
	mock.ExpectCopyFrom([]string{"etcd"}, []string{"ts", "key", "value", "revision", "tombstone"}).WillReturnResult(1)

	value := "value1"
	records := []KeyValueRecord{
		{
			Key:       "test/key1",
			Value:     &value,
			Revision:  1,
			Timestamp: "2023-01-01T00:00:00Z",
			Tombstone: false,
		},
	}

	// Test the function
	err = BulkInsert(ctx, mock, records)
	require.NoError(t, err)

	// Verify all expectations were met
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestInsertWALEntry tests WAL entry insertion with mock
func TestInsertWALEntry(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()
	key := "test/key"
	value := "value"
	revision := int64(1)

	// Set up mock expectations
	mock.ExpectExec("INSERT INTO etcd_wal").
		WithArgs(key, &value, &revision).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Test the function
	err = InsertWALEntry(ctx, mock, key, &value, &revision)
	require.NoError(t, err)

	// Verify all expectations were met
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetPendingWALEntries tests retrieving pending WAL entries with mock
func TestGetPendingWALEntries(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	// Set up mock expectations
	rows := mock.NewRows([]string{"key", "value", "revision", "ts"}).
		AddRow("test/key1", "value1", int64(1), "2023-01-01T00:00:00Z").
		AddRow("test/key2", nil, nil, "2023-01-01T00:01:00Z")

	mock.ExpectQuery("SELECT key, value, revision, ts FROM etcd_wal").
		WillReturnRows(rows)

	// Test the function
	entries, err := GetPendingWALEntries(ctx, mock)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	assert.Equal(t, "test/key1", entries[0].Key)
	require.NotNil(t, entries[0].Value)
	assert.Equal(t, "value1", *entries[0].Value)
	require.NotNil(t, entries[0].Revision)
	assert.Equal(t, int64(1), *entries[0].Revision)

	assert.Equal(t, "test/key2", entries[1].Key)
	assert.Nil(t, entries[1].Value)
	assert.Nil(t, entries[1].Revision)

	// Verify all expectations were met
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestDeleteWALEntry tests deleting WAL entry with mock
func TestDeleteWALEntry(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	// Set up mock expectations
	mock.ExpectExec("UPDATE etcd_wal").
		WithArgs("test/key", "2023-01-01T00:00:00Z", int64(-1)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Test the function
	err = UpdateWALEntry(ctx, mock, "test/key", "2023-01-01T00:00:00Z", -1)
	require.NoError(t, err)

	// Verify all expectations were met
	require.NoError(t, mock.ExpectationsWereMet())
} // TestGetLatestRevision tests getting latest revision with mock
func TestGetLatestRevision(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	// Test case: no records (NULL result)
	mock.ExpectQuery("SELECT MAX\\(revision\\) FROM etcd").
		WillReturnRows(mock.NewRows([]string{"max"}).AddRow(nil))

	revision, err := GetLatestRevision(ctx, mock)
	require.NoError(t, err)
	assert.Equal(t, int64(0), revision)

	// Test case: with records
	mock.ExpectQuery("SELECT MAX\\(revision\\) FROM etcd").
		WillReturnRows(mock.NewRows([]string{"max"}).AddRow(42))

	revision, err = GetLatestRevision(ctx, mock)
	require.NoError(t, err)
	assert.Equal(t, int64(42), revision)

	// Verify all expectations were met
	require.NoError(t, mock.ExpectationsWereMet())
}
