// Package sync provides consolidated PostgreSQL operations for etcd synchronization.
package sync

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/cybertec-postgresql/etcd_fdw/internal/migrations"
)

// PgxIface is common interface for every pgx class
type PgxIface interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(ctx context.Context, query string, args ...any) (pgx.Rows, error)
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// New creates new connection from PostgreSQL URL
func New(ctx context.Context, databaseURL string, callbacks ...func(*pgxpool.Config) error) (*pgxpool.Pool, error) {
	connConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Set up connection callbacks
	logger := logrus.WithField("component", "postgresql")
	connConfig.ConnConfig.OnNotice = func(_ *pgconn.PgConn, n *pgconn.Notice) {
		logger.WithField("severity", n.Severity).WithField("notice", n.Message).Info("Notice received")
	}
	for _, f := range callbacks {
		if err := f(connConfig); err != nil {
			return nil, err
		}
	}
	return pgxpool.NewWithConfig(ctx, connConfig)
}

// ApplyMigrations checks and applies database migrations if needed
func ApplyMigrations(ctx context.Context, conn *pgx.Conn) error {
	needsMigration, err := migrations.NeedsUpgrade(ctx, conn)
	if err != nil {
		return fmt.Errorf("failed to check migration status: %w", err)
	}

	if needsMigration {
		logrus.Info("Applying database migrations...")
		err = migrations.Apply(ctx, conn)
		if err != nil {
			return fmt.Errorf("failed to apply migrations: %w", err)
		}
		logrus.Info("Database migrations completed successfully")
	} else {
		logrus.Info("Database schema is up to date")
	}

	return nil
}

// BulkInsert performs bulk insert of key-value records using INSERT ON CONFLICT with pgx.Batch
func BulkInsert(ctx context.Context, pool PgxIface, records []KeyValueRecord) error {
	if len(records) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	query := `INSERT INTO etcd (ts, key, value, revision, tombstone) 
			  VALUES ($1, $2, $3, $4, $5) 
			  ON CONFLICT (key, revision) DO UPDATE SET 
			  ts = EXCLUDED.ts, value = EXCLUDED.value, tombstone = EXCLUDED.tombstone`

	for _, record := range records {
		if record.Tombstone {
			record.Value = "" // Insert empty for tombstones
		}
		batch.Queue(query, record.Ts, record.Key, record.Value, record.Revision, record.Tombstone)
	}

	if err := pool.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("failed to execute batch insert: %w", err)
	}

	logrus.WithField("count", len(records)).Info("Bulk inserted/updated records to PostgreSQL")
	return nil
}

// GetPendingRecords retrieves records that need to be synced to etcd (revision = -1)
func GetPendingRecords(ctx context.Context, pool PgxIface) ([]KeyValueRecord, error) {
	query := `SELECT key, value, revision, ts, tombstone
		FROM etcd 
		WHERE revision = -1
		ORDER BY ts ASC`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending records: %w", err)
	}
	defer rows.Close()

	var records []KeyValueRecord
	for rows.Next() {
		var record KeyValueRecord
		var value *string

		err := rows.Scan(&record.Key, &value, &record.Revision, &record.Ts, &record.Tombstone)
		if err != nil {
			return nil, fmt.Errorf("error scanning pending record: %w", err)
		}

		// Handle NULL value for tombstones
		if value != nil {
			record.Value = *value
		} else {
			record.Value = ""
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating pending records: %w", err)
	}

	return records, nil
}

// UpdateRevision updates the revision of a record after successful sync to etcd
func UpdateRevision(ctx context.Context, pool PgxIface, key string, revision int64) error {
	query := `UPDATE etcd SET revision = $2 WHERE key = $1 AND revision = -1`

	result, err := pool.Exec(ctx, query, key, revision)
	if err != nil {
		return fmt.Errorf("failed to update revision: %w", err)
	}

	rowsAffected := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("no pending record found for key %s", key)
	}

	return nil
}

// GetLatestRevision returns the highest revision number in the etcd table
func GetLatestRevision(ctx context.Context, pool PgxIface) (int64, error) {
	var revision *int64

	query := `SELECT MAX(revision) FROM etcd WHERE revision > 0`
	err := pool.QueryRow(ctx, query).Scan(&revision)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest revision: %w", err)
	}

	if revision == nil {
		return 0, nil // No records yet
	}

	return *revision, nil
}

// NewWithRetry creates a new PostgreSQL connection pool with retry logic
func NewWithRetry(ctx context.Context, databaseURL string, callbacks ...func(*pgxpool.Config) error) (*pgxpool.Pool, error) {
	config := DefaultRetryConfig()

	var pool *pgxpool.Pool
	err := RetryWithBackoff(ctx, config, func() error {
		var attemptErr error
		pool, attemptErr = New(ctx, databaseURL, callbacks...)
		if attemptErr != nil {
			return attemptErr
		}

		// Test the connection with a ping
		if pingErr := pool.Ping(ctx); pingErr != nil {
			if pool != nil {
				pool.Close()
			}
			return pingErr
		}

		return nil
	})

	if err != nil {
		logrus.WithError(err).Error("Failed to establish PostgreSQL connection after all retries")
		return nil, err
	}

	return pool, nil
}

// InsertPendingRecord inserts a new record with revision -1 (pending sync to etcd)
func InsertPendingRecord(ctx context.Context, pool PgxIface, key string, value string, tombstone bool) error {
	query := `
		INSERT INTO etcd (key, value, revision, tombstone)
		VALUES ($1, $2, -1, $3) 
		ON CONFLICT (key, revision) DO UPDATE 
		SET value = EXCLUDED.value, ts = CURRENT_TIMESTAMP, tombstone = EXCLUDED.tombstone;
	`
	if tombstone {
		value = "" // Use empty string for tombstone records
	}
	_, err := pool.Exec(ctx, query, key, value, tombstone)
	if err != nil {
		return fmt.Errorf("failed to insert pending record: %w", err)
	}

	return nil
}
