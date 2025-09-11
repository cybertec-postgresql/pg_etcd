// Package db provides PostgreSQL database operations for etcd synchronization.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

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

// BulkInsert performs bulk insert of key-value records using COPY
func BulkInsert(ctx context.Context, pool PgxIface, records []KeyValueRecord) error {
	// Use COPY for efficient bulk insert
	_, err := pool.CopyFrom(
		ctx,
		pgx.Identifier{"etcd"},
		[]string{"ts", "key", "value", "revision", "tombstone"},
		pgx.CopyFromSlice(len(records), func(i int) ([]any, error) {
			return []any{records[i].Ts, records[i].Key, records[i].Value, records[i].Revision, records[i].Tombstone}, nil
		}),
	)

	if err != nil {
		return fmt.Errorf("failed to bulk insert records: %w", err)
	}

	logrus.WithField("count", len(records)).Info("Bulk inserted records to PostgreSQL")
	return nil
}

// KeyValueRecord represents a key-value record in the etcd table with revision status encoding
type KeyValueRecord struct {
	Key       string
	Value     string // nullable for tombstones
	Revision  int64  // -1 = pending (needs sync to etcd), >0 = synced from etcd
	Ts        time.Time
	Tombstone bool
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

	records, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[KeyValueRecord]) // ensure rows are closed in case of early return
	if err != nil {
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

// InsertPendingRecord inserts a new record with revision -1 (pending sync to etcd)
func InsertPendingRecord(ctx context.Context, pool PgxIface, key string, value string, tombstone bool) error {
	query := `
		INSERT INTO etcd (key, value, revision, tombstone)
		VALUES ($1, $2, -1, $3) 
		ON CONFLICT (key, revision) DO UPDATE 
		SET value = EXCLUDED.value, ts = CURRENT_TIMESTAMP, tombstone = EXCLUDED.tombstone;
	`

	_, err := pool.Exec(ctx, query, key, value, tombstone)
	if err != nil {
		return fmt.Errorf("failed to insert pending record: %w", err)
	}

	return nil
}

// GetLatestRevision returns the highest revision number in the etcd table
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
