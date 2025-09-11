// Package db provides PostgreSQL database testing for etcd_fdw.
package db

import (
	"context"
	"testing"
	"time"

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
			Value:     value,
			Revision:  1,
			Ts:        time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
			Tombstone: false,
		},
	}

	// Test the function
	err = BulkInsert(ctx, mock, records)
	require.NoError(t, err)

	// Verify all expectations were met
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestGetPendingRecords tests retrieving pending records with mock
func TestGetPendingRecords(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	// Set up mock expectations
	rows := mock.NewRows([]string{"key", "value", "revision", "ts", "tombstone"}).
		AddRow("test/key1", "value1", int64(-1), time.Now(), false).
		AddRow("test/key2", nil, int64(-1), time.Now(), true)

	mock.ExpectQuery("SELECT key, value, revision, ts, tombstone FROM etcd WHERE revision = -1").
		WillReturnRows(rows)

	// Test the function
	records, err := GetPendingRecords(ctx, mock)
	require.NoError(t, err)
	require.Len(t, records, 2)

	assert.Equal(t, "test/key1", records[0].Key)
	require.NotNil(t, records[0].Value)
	assert.Equal(t, "value1", records[0].Value)
	assert.Equal(t, int64(-1), records[0].Revision)
	assert.False(t, records[0].Tombstone)

	assert.Equal(t, "test/key2", records[1].Key)
	assert.Empty(t, records[1].Value)
	assert.Equal(t, int64(-1), records[1].Revision)
	assert.True(t, records[1].Tombstone)

	// Verify all expectations were met
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateRevision tests updating revision with mock
func TestUpdateRevision(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	// Set up mock expectations
	mock.ExpectExec("UPDATE etcd SET revision = \\$2 WHERE key = \\$1 AND revision = -1").
		WithArgs("test/key", int64(123)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	// Test the function
	err = UpdateRevision(ctx, mock, "test/key", 123)
	require.NoError(t, err)

	// Verify all expectations were met
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestInsertPendingRecord tests inserting pending records with mock
func TestInsertPendingRecord(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()
	key := "test/key"
	value := "value"

	// Set up mock expectations
	mock.ExpectExec("INSERT INTO etcd \\(key, value, revision, tombstone\\)").
		WithArgs(key, value, false).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Test the function
	err = InsertPendingRecord(ctx, mock, key, value, false)
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
