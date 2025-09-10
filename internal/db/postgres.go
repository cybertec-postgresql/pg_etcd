// Package db provides PostgreSQL database operations for etcd synchronization.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"github.com/cybertec-postgresql/etcd_fdw/internal/migrations"
)

// PgxIface is common interface for every pgx class
type PgxIface interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
	Query(ctx context.Context, query string, args ...interface{}) (pgx.Rows, error)
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
}

// PgxConnIface is interface representing pgx connection
type PgxConnIface interface {
	PgxIface
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	Close(ctx context.Context) error
	Ping(ctx context.Context) error
}

// PgxPoolIface is interface representing pgx pool
type PgxPoolIface interface {
	PgxIface
	Acquire(ctx context.Context) (*pgxpool.Conn, error)
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
	Close()
	Config() *pgxpool.Config
	Ping(ctx context.Context) error
	Stat() *pgxpool.Stat
}

type ConnConfigCallback = func(*pgxpool.Config) error

// New create a new pool
func New(ctx context.Context, connStr string, callbacks ...ConnConfigCallback) (PgxPoolIface, error) {
	connConfig, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, err
	}
	return NewWithConfig(ctx, connConfig, callbacks...)
}

// NewWithConfig creates a new pool with a given config
func NewWithConfig(ctx context.Context, connConfig *pgxpool.Config, callbacks ...ConnConfigCallback) (PgxPoolIface, error) {
	logger := logrus.StandardLogger()
	if connConfig.ConnConfig.ConnectTimeout == 0 {
		connConfig.ConnConfig.ConnectTimeout = time.Second * 5
	}
	connConfig.MaxConnIdleTime = 15 * time.Second
	connConfig.ConnConfig.RuntimeParams["application_name"] = "etcd_fdw"
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

// SetupListen sets up PostgreSQL LISTEN for WAL notifications
func SetupListen(ctx context.Context, pool PgxPoolIface, channel string) (*pgx.Conn, error) {
	// Get DSN from the pool config
	config := pool.Config()
	dsn := config.ConnString()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to create LISTEN connection: %w", err)
	}

	_, err = conn.Exec(ctx, "LISTEN "+channel)
	if err != nil {
		conn.Close(ctx)
		return nil, fmt.Errorf("failed to setup LISTEN: %w", err)
	}

	logrus.WithField("channel", channel).Info("PostgreSQL LISTEN setup successfully")
	return conn, nil
}

// BulkInsert performs bulk insert of key-value records using COPY
func BulkInsert(ctx context.Context, pool PgxIface, records []KeyValueRecord) error {
	// Prepare data for COPY
	rows := make([][]interface{}, len(records))
	for i, record := range records {
		rows[i] = []interface{}{
			record.Timestamp,
			record.Key,
			record.Value,
			record.Revision,
			record.Tombstone,
		}
	}

	// Use COPY for efficient bulk insert
	_, err := pool.CopyFrom(
		ctx,
		pgx.Identifier{"etcd"},
		[]string{"ts", "key", "value", "revision", "tombstone"},
		pgx.CopyFromRows(rows),
	)

	if err != nil {
		return fmt.Errorf("failed to bulk insert records: %w", err)
	}

	logrus.WithField("count", len(records)).Info("Bulk inserted records to PostgreSQL")
	return nil
}

// KeyValueRecord represents a key-value record in the etcd table
type KeyValueRecord struct {
	Key       string
	Value     *string // nullable for tombstones
	Revision  int64
	Timestamp string
	Tombstone bool
}

// WALEntry represents an entry in the etcd_wal table
type WALEntry struct {
	Key       string
	Value     *string // nullable for deletes
	Revision  *int64  // nullable for new keys
	Timestamp string
}

// InsertWALEntry adds a new entry to the etcd_wal table
func InsertWALEntry(ctx context.Context, pool PgxIface, key string, value *string, revision *int64) error {
	query := `
		INSERT INTO etcd_wal (key, value, revision)
		VALUES ($1, $2, $3)
	`

	_, err := pool.Exec(ctx, query, key, value, revision)
	if err != nil {
		return fmt.Errorf("failed to insert WAL entry: %w", err)
	}

	return nil
}

// GetPendingWALEntries retrieves WAL entries that need to be processed
func GetPendingWALEntries(ctx context.Context, pool PgxIface) ([]WALEntry, error) {
	query := `
		SELECT key, value, revision, ts
		FROM etcd_wal
		ORDER BY ts ASC
	`

	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query WAL entries: %w", err)
	}
	defer rows.Close()

	var entries []WALEntry
	for rows.Next() {
		var entry WALEntry
		var value pgtype.Text
		var revision pgtype.Int8

		err := rows.Scan(&entry.Key, &value, &revision, &entry.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to scan WAL entry: %w", err)
		}

		if value.Valid {
			entry.Value = &value.String
		}
		if revision.Valid {
			entry.Revision = &revision.Int64
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating WAL entries: %w", err)
	}

	return entries, nil
}

// DeleteWALEntry removes a processed WAL entry
func DeleteWALEntry(ctx context.Context, pool PgxIface, key string, timestamp string) error {
	query := `DELETE FROM etcd_wal WHERE key = $1 AND ts = $2`

	_, err := pool.Exec(ctx, query, key, timestamp)
	if err != nil {
		return fmt.Errorf("failed to delete WAL entry: %w", err)
	}

	return nil
} // GetLatestRevision returns the highest revision number in the etcd table
func GetLatestRevision(ctx context.Context, pool PgxIface) (int64, error) {
	var revision sql.NullInt64

	query := `SELECT MAX(revision) FROM etcd`
	err := pool.QueryRow(ctx, query).Scan(&revision)
	if err != nil {
		return 0, fmt.Errorf("failed to get latest revision: %w", err)
	}

	if !revision.Valid {
		return 0, nil // No records yet
	}

	return revision.Int64, nil
}
