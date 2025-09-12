// Package sync provides consolidated PostgreSQL operations for etcd synchronization.
package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/cybertec-postgresql/etcd_fdw/internal/migrations"
	"github.com/cybertec-postgresql/etcd_fdw/internal/retry"
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

// KeyValueRecord represents a key-value record in the etcd table with revision status encoding
type KeyValueRecord struct {
	Key       string
	Value     string // nullable for tombstones in database, empty string in code
	Revision  int64  // -1 for pending sync to etcd, >0 for real etcd revision
	Ts        time.Time
	Tombstone bool
}

// PoolSettings contains configuration for PostgreSQL connection pools
type PoolSettings struct {
	Host         string
	Port         int
	Database     string
	User         string
	Password     string
	SSLMode      string
	MaxConns     int32
	MinConns     int32
	MaxConnLife  time.Duration
	MaxConnIdle  time.Duration
	HealthCheck  time.Duration
	ConnAttempts int
}

// DefaultPoolSettings returns sensible defaults for PostgreSQL connection pooling
func DefaultPoolSettings() PoolSettings {
	return PoolSettings{
		Host:         "localhost",
		Port:         5432,
		Database:     "postgres",
		User:         "postgres",
		SSLMode:      "prefer",
		MaxConns:     30,
		MinConns:     0,
		MaxConnLife:  time.Hour,
		MaxConnIdle:  time.Minute * 30,
		HealthCheck:  time.Minute,
		ConnAttempts: 10,
	}
}

// New creates new connection from PostgreSQL URL with default configuration
func New(ctx context.Context, databaseURL string, callbacks ...func(*pgxpool.Config) error) (*pgxpool.Pool, error) {
	return NewWithConfig(ctx, databaseURL, DefaultPoolSettings(), callbacks...)
}

// NewWithConfig creates a new PostgreSQL connection pool with the given configuration
func NewWithConfig(ctx context.Context, databaseURL string, settings PoolSettings, callbacks ...func(*pgxpool.Config) error) (*pgxpool.Pool, error) {
	connConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Apply pool settings
	connConfig.MaxConns = settings.MaxConns
	connConfig.MinConns = settings.MinConns
	connConfig.MaxConnLifetime = settings.MaxConnLife
	connConfig.MaxConnIdleTime = settings.MaxConnIdle
	connConfig.HealthCheckPeriod = settings.HealthCheck

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
		var value interface{}
		if record.Tombstone {
			value = nil // Insert NULL for tombstones
		} else {
			value = record.Value
		}
		batch.Queue(query, record.Ts, record.Key, value, record.Revision, record.Tombstone)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < len(records); i++ {
		_, err := br.Exec()
		if err != nil {
			return fmt.Errorf("failed to execute batch insert for record %d: %w", i, err)
		}
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
	config := retry.PostgreSQLDefaults()

	var pool *pgxpool.Pool
	err := retry.WithOperation(ctx, config, func() error {
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
	}, "PostgreSQL connect")

	if err != nil {
		logrus.WithError(err).Error("Failed to establish PostgreSQL connection after all retries")
		return nil, err
	}

	return pool, nil
}

// RetryOperation retries a database operation with exponential backoff
func RetryOperation(ctx context.Context, operation func() error, operationName string) error {
	config := retry.PostgreSQLDefaults()
	return retry.WithOperation(ctx, config, operation, operationName)
}

// InsertPendingRecord inserts a new record with revision -1 (pending sync to etcd)
func InsertPendingRecord(ctx context.Context, pool PgxIface, key string, value string, tombstone bool) error {
	query := `
		INSERT INTO etcd (key, value, revision, tombstone)
		VALUES ($1, $2, -1, $3) 
		ON CONFLICT (key, revision) DO UPDATE 
		SET value = EXCLUDED.value, ts = CURRENT_TIMESTAMP, tombstone = EXCLUDED.tombstone;
	`

	var valueParam interface{}
	if tombstone {
		valueParam = nil // Insert NULL for tombstones
	} else {
		valueParam = value
	}

	_, err := pool.Exec(ctx, query, key, valueParam, tombstone)
	if err != nil {
		return fmt.Errorf("failed to insert pending record: %w", err)
	}

	return nil
}
