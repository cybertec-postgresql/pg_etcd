package sync

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupPostgreSQLContainer(ctx context.Context, t *testing.T) (*pgxpool.Pool, testcontainers.Container) {
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

	pgConnStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, pgConnStr)
	require.NoError(t, err)

	// Create etcd table with single table architecture
	_, err = pool.Exec(ctx, `
		CREATE TABLE etcd (
			ts timestamp with time zone NOT NULL DEFAULT now(),
			key text NOT NULL,
			value text NOT NULL,
			revision bigint NOT NULL,
			tombstone boolean NOT NULL DEFAULT false,
			PRIMARY KEY(key, revision)
		);
		CREATE INDEX idx_etcd_pending ON etcd(key) WHERE revision = -1;
		CREATE INDEX idx_etcd_ts ON etcd(ts);
	`)
	require.NoError(t, err)

	return pool, pgContainer
}

func setupEtcdContainer(ctx context.Context, t *testing.T) (*EtcdClient, testcontainers.Container) {
	etcdContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "quay.io/coreos/etcd:v3.5.9",
			ExposedPorts: []string{"2379/tcp"},
			Env: map[string]string{
				"ETCD_ADVERTISE_CLIENT_URLS":       "http://0.0.0.0:2379",
				"ETCD_LISTEN_CLIENT_URLS":          "http://0.0.0.0:2379",
				"ETCD_LISTEN_PEER_URLS":            "http://0.0.0.0:2380",
				"ETCD_INITIAL_ADVERTISE_PEER_URLS": "http://0.0.0.0:2380",
				"ETCD_INITIAL_CLUSTER":             "default=http://0.0.0.0:2380",
				"ETCD_NAME":                        "default",
			},
			WaitingFor: wait.ForListeningPort("2379/tcp"),
		},
		Started: true,
	})
	require.NoError(t, err)

	endpoint, err := etcdContainer.Endpoint(ctx, "")
	require.NoError(t, err)

	dsn := "etcd://" + endpoint + "/test"
	etcdClient, err := NewEtcdClient(dsn)
	require.NoError(t, err)

	return etcdClient, etcdContainer
}

func setupTestContainers(t *testing.T) (*pgxpool.Pool, *EtcdClient, func()) {
	ctx := context.Background()

	pool, pgContainer := setupPostgreSQLContainer(ctx, t)
	etcdClient, etcdContainer := setupEtcdContainer(ctx, t)

	cleanup := func() {
		pool.Close()
		_ = etcdClient.Close()
		_ = pgContainer.Terminate(ctx)
		_ = etcdContainer.Terminate(ctx)
	}

	return pool, etcdClient, cleanup
}

func TestPollingMechanism(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool, _, cleanup := setupTestContainers(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Insert test record with revision = -1 (pending sync)
	_, err := pool.Exec(ctx, `
		INSERT INTO etcd (key, value, revision, tombstone) 
		VALUES ('test/polling/key1', 'value1', -1, false)
	`)
	require.NoError(t, err)

	// Test GetPendingRecords function
	pendingRecords, err := GetPendingRecords(ctx, pool)
	require.NoError(t, err)
	assert.Len(t, pendingRecords, 1)
	assert.Equal(t, "test/polling/key1", pendingRecords[0].Key)
	assert.Equal(t, "value1", pendingRecords[0].Value)

	// Test UpdateRevision function
	err = UpdateRevision(ctx, pool, "test/polling/key1", 123)
	require.NoError(t, err)

	// Verify record was updated
	pendingAfterUpdate, err := GetPendingRecords(ctx, pool)
	require.NoError(t, err)
	assert.Len(t, pendingAfterUpdate, 0, "No pending records should remain after update")

	// Verify record exists with correct revision
	var revision int64
	err = pool.QueryRow(ctx, `
		SELECT revision FROM etcd 
		WHERE key = 'test/polling/key1' AND revision = 123
	`).Scan(&revision)
	require.NoError(t, err)
	assert.Equal(t, int64(123), revision)
}

func TestBulkInsert(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool, _, cleanup := setupTestContainers(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Prepare test records
	records := []KeyValueRecord{
		{
			Key:       "test/bulk/key1",
			Value:     ("value1"),
			Revision:  100,
			Ts:        time.Now(),
			Tombstone: false,
		},
		{
			Key:       "test/bulk/key2",
			Value:     ("value2"),
			Revision:  101,
			Ts:        time.Now(),
			Tombstone: false,
		},
		{
			Key:       "test/bulk/key3",
			Value:     "",
			Revision:  102,
			Ts:        time.Now(),
			Tombstone: true,
		},
	}

	// Test BulkInsert function
	err := BulkInsert(ctx, pool, records)
	require.NoError(t, err)

	// Verify records were inserted correctly
	var count int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM etcd WHERE key LIKE 'test/bulk/%'`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 3, count)

	// Verify specific record details
	var key, value string
	var revision int64
	var tombstone bool
	err = pool.QueryRow(ctx, `
		SELECT key, value, revision, tombstone 
		FROM etcd WHERE key = 'test/bulk/key1'
	`).Scan(&key, &value, &revision, &tombstone)
	require.NoError(t, err)
	assert.Equal(t, "test/bulk/key1", key)
	assert.NotEmpty(t, value)
	assert.Equal(t, "value1", value)
	assert.Equal(t, int64(100), revision)
	assert.False(t, tombstone)

	// Verify tombstone record
	err = pool.QueryRow(ctx, `
		SELECT key, value, revision, tombstone 
		FROM etcd WHERE key = 'test/bulk/key3'
	`).Scan(&key, &value, &revision, &tombstone)
	require.NoError(t, err)
	assert.Equal(t, "test/bulk/key3", key)
	assert.Empty(t, value) // NULL value
	assert.Equal(t, int64(102), revision)
	assert.True(t, tombstone)
}

func TestInsertPendingRecord(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool, _, cleanup := setupTestContainers(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test inserting a new pending record
	err := InsertPendingRecord(ctx, pool, "test/pending/key1", ("value1"), false)
	require.NoError(t, err)

	// Verify record was inserted with revision = -1
	var revision int64
	var value string
	err = pool.QueryRow(ctx, `
		SELECT revision, value FROM etcd 
		WHERE key = 'test/pending/key1'
	`).Scan(&revision, &value)
	require.NoError(t, err)
	assert.Equal(t, int64(-1), revision)
	assert.NotEmpty(t, value)
	assert.Equal(t, "value1", value)

	// Test inserting second record with same key (should create new record with different timestamp)
	err = InsertPendingRecord(ctx, pool, "test/pending/key1", ("updated_value"), false)
	require.NoError(t, err)

	// Verify both records exist (different timestamps, both with revision = -1)
	var count int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM etcd 
		WHERE key = 'test/pending/key1' AND revision = -1
	`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Should have 1 pending records for the same key with latest value")

	// Test inserting tombstone record
	err = InsertPendingRecord(ctx, pool, "test/pending/key2", "", true)
	require.NoError(t, err)

	// Verify tombstone record
	var tombstone bool
	err = pool.QueryRow(ctx, `
		SELECT revision, value, tombstone FROM etcd 
		WHERE key = 'test/pending/key2'
	`).Scan(&revision, &value, &tombstone)
	require.NoError(t, err)
	assert.Equal(t, int64(-1), revision)
	assert.Empty(t, value)
	assert.True(t, tombstone)
}

func TestGetLatestRevision(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool, _, cleanup := setupTestContainers(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test with empty table
	latestRevision, err := GetLatestRevision(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, int64(0), latestRevision)

	// Insert records with different revisions
	_, err = pool.Exec(ctx, `
		INSERT INTO etcd (key, value, revision) VALUES 
		('test/rev/key1', 'value1', 100),
		('test/rev/key2', 'value2', 50),
		('test/rev/key3', 'value3', 150),
		('test/rev/key4', 'value4', -1)
	`)
	require.NoError(t, err)

	// Test latest revision (should ignore -1 pending records)
	latestRevision, err = GetLatestRevision(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, int64(150), latestRevision)
}

func TestPendingRecordFiltering(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool, _, cleanup := setupTestContainers(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Insert mixed records (synced and pending)
	_, err := pool.Exec(ctx, `
		INSERT INTO etcd (key, value, revision, tombstone) VALUES 
		('test/filter/synced1', 'value1', 100, false),
		('test/filter/pending1', 'value2', -1, false),
		('test/filter/synced2', 'value3', 200, false),
		('test/filter/pending2', '', -1, true),
		('test/filter/pending3', 'value4', -1, false)
	`)
	require.NoError(t, err)

	// Test GetPendingRecords only returns revision = -1
	pendingRecords, err := GetPendingRecords(ctx, pool)
	require.NoError(t, err)
	assert.Len(t, pendingRecords, 3)

	// Verify only pending records are returned
	pendingKeys := make([]string, len(pendingRecords))
	for i, record := range pendingRecords {
		pendingKeys[i] = record.Key
		assert.Equal(t, int64(-1), record.Revision)
	}
	assert.Contains(t, pendingKeys, "test/filter/pending1")
	assert.Contains(t, pendingKeys, "test/filter/pending2")
	assert.Contains(t, pendingKeys, "test/filter/pending3")

	// Verify tombstone record is handled correctly
	for _, record := range pendingRecords {
		if record.Key == "test/filter/pending2" {
			assert.True(t, record.Tombstone)
			assert.Equal(t, "", record.Value) // Empty string for tombstones
		}
	}
}

func TestConflictResolution(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	pool, _, cleanup := setupTestContainers(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Insert a pending record
	err := InsertPendingRecord(ctx, pool, "test/conflict/key1", "pending_value", false)
	require.NoError(t, err)

	// Verify it's pending
	pendingRecords, err := GetPendingRecords(ctx, pool)
	require.NoError(t, err)
	assert.Len(t, pendingRecords, 1)
	assert.Equal(t, "test/conflict/key1", pendingRecords[0].Key)
	assert.Equal(t, int64(-1), pendingRecords[0].Revision)

	// Simulate etcd sync by updating revision
	err = UpdateRevision(ctx, pool, "test/conflict/key1", 300)
	require.NoError(t, err)

	// Verify record is no longer pending
	pendingAfterUpdate, err := GetPendingRecords(ctx, pool)
	require.NoError(t, err)
	assert.Len(t, pendingAfterUpdate, 0)

	// Verify record has correct revision
	var revision int64
	err = pool.QueryRow(ctx, `
		SELECT revision FROM etcd 
		WHERE key = 'test/conflict/key1'
	`).Scan(&revision)
	require.NoError(t, err)
	assert.Equal(t, int64(300), revision)

	// Test updating non-existent pending record (should fail gracefully)
	err = UpdateRevision(ctx, pool, "test/conflict/nonexistent", 400)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no pending record found")
}

func TestPerformanceOpsPerSecond(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	pool, _, cleanup := setupTestContainers(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Insert 1000 test records
	recordCount := 1000
	start := time.Now()

	records := make([]KeyValueRecord, recordCount)
	for i := 0; i < recordCount; i++ {
		value := fmt.Sprintf("test_value_%d", i)
		records[i] = KeyValueRecord{
			Key:       fmt.Sprintf("test/perf/key%d", i),
			Value:     value,
			Revision:  int64(i + 1),
			Ts:        time.Now(),
			Tombstone: false,
		}
	}

	err := BulkInsert(ctx, pool, records)
	require.NoError(t, err)

	elapsed := time.Since(start)
	opsPerSecond := float64(recordCount) / elapsed.Seconds()

	t.Logf("Inserted %d records in %v (%.2f ops/sec)", recordCount, elapsed, opsPerSecond)
	assert.GreaterOrEqual(t, opsPerSecond, 1000.0, "Should achieve at least 1000 ops/sec")
}

func TestPerformanceSyncLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	pool, _, cleanup := setupTestContainers(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test individual operation latency
	iterations := 10
	totalLatency := time.Duration(0)

	for i := 0; i < iterations; i++ {
		start := time.Now()

		// Insert pending record
		key := fmt.Sprintf("test/latency/key%d", i)
		value := fmt.Sprintf("test_value_%d", i)
		err := InsertPendingRecord(ctx, pool, key, value, false)
		require.NoError(t, err)

		// Update revision (simulating sync completion)
		err = UpdateRevision(ctx, pool, key, int64(i+1))
		require.NoError(t, err)

		latency := time.Since(start)
		totalLatency += latency
	}

	avgLatency := totalLatency / time.Duration(iterations)
	t.Logf("Average sync latency: %v", avgLatency)
	assert.Less(t, avgLatency, 100*time.Millisecond, "Average sync latency should be under 100ms")
}
