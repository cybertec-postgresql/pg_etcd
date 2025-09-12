package sync

import (
	"context"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBulkInsert tests bulk insert operation with pgxmock (simplified)
func TestBulkInsert(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()

	records := []KeyValueRecord{
		{Ts: now, Key: "key1", Value: "value1", Revision: 1, Tombstone: false},
		{Ts: now, Key: "key2", Value: "", Revision: 1, Tombstone: true},
	}
	b := mock.ExpectBatch()
	b.ExpectExec("INSERT").WithArgs(pgxmock.AnyArg(), "key1", "value1", int64(1), false).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	b.ExpectExec("INSERT").WithArgs(pgxmock.AnyArg(), "key2", "", int64(1), true).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	err = BulkInsert(ctx, mock, records)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetPendingRecords tests retrieval of pending records with pgxmock
func TestGetPendingRecords(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()
	now := time.Now()

	valuePtr := "value1"
	rows := pgxmock.NewRows([]string{"key", "value", "revision", "ts", "tombstone"}).
		AddRow("pending1", &valuePtr, int64(-1), now, false).
		AddRow("pending2", (*string)(nil), int64(-1), now, true)

	mock.ExpectQuery(`SELECT key, value, revision, ts, tombstone FROM etcd WHERE revision = -1 ORDER BY ts ASC`).
		WillReturnRows(rows)

	records, err := GetPendingRecords(ctx, mock)
	require.NoError(t, err)
	assert.Len(t, records, 2)

	assert.Equal(t, "pending1", records[0].Key)
	assert.Equal(t, "value1", records[0].Value)
	assert.Equal(t, int64(-1), records[0].Revision)
	assert.False(t, records[0].Tombstone)

	assert.Equal(t, "pending2", records[1].Key)
	assert.Equal(t, "", records[1].Value) // NULL becomes empty string
	assert.Equal(t, int64(-1), records[1].Revision)
	assert.True(t, records[1].Tombstone)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestUpdateRevision tests revision update with pgxmock
func TestUpdateRevision(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectExec(`UPDATE etcd SET revision = \$2 WHERE key = \$1 AND revision = -1`).
		WithArgs("test-key", int64(123)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = UpdateRevision(ctx, mock, "test-key", 123)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestUpdateRevisionNotFound tests revision update when no record found
func TestUpdateRevisionNotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	mock.ExpectExec(`UPDATE etcd SET revision = \$2 WHERE key = \$1 AND revision = -1`).
		WithArgs("missing-key", int64(123)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err = UpdateRevision(ctx, mock, "missing-key", 123)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no pending record found")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestGetLatestRevision tests getting latest revision with pgxmock
func TestGetLatestRevision(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	// Test with existing revisions
	revisionValue := int64(456)
	rows := pgxmock.NewRows([]string{"max"}).AddRow(&revisionValue)
	mock.ExpectQuery(`SELECT MAX\(revision\) FROM etcd WHERE revision > 0`).
		WillReturnRows(rows)

	revision, err := GetLatestRevision(ctx, mock)
	assert.NoError(t, err)
	assert.Equal(t, int64(456), revision)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestGetLatestRevisionEmpty tests getting latest revision when no records exist
func TestGetLatestRevisionEmpty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	// Test with no revisions (NULL result)
	rows := pgxmock.NewRows([]string{"max"}).AddRow((*int64)(nil))
	mock.ExpectQuery(`SELECT MAX\(revision\) FROM etcd WHERE revision > 0`).
		WillReturnRows(rows)

	revision, err := GetLatestRevision(ctx, mock)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), revision)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestInsertPendingRecord tests inserting pending record with pgxmock
func TestInsertPendingRecord(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	// Test normal record insert
	mock.ExpectExec(`INSERT INTO etcd \(key, value, revision, tombstone\)`).
		WithArgs("test-key", "test-value", false).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = InsertPendingRecord(ctx, mock, "test-key", "test-value", false)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

// TestInsertPendingRecordTombstone tests inserting tombstone record with pgxmock
func TestInsertPendingRecordTombstone(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx := context.Background()

	// Test tombstone record insert (value should be nil)
	mock.ExpectExec(`INSERT INTO etcd \(key, value, revision, tombstone\)`).
		WithArgs("test-key", nil, true).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = InsertPendingRecord(ctx, mock, "test-key", "test-value", true)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}
