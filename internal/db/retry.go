// Package retry provides connection retry logic with exponential backoff for PostgreSQL.
package db

import (
	"context"

	"github.com/cybertec-postgresql/etcd_fdw/internal/retry"
	"github.com/sirupsen/logrus"
)

// NewWithRetry creates a new PostgreSQL connection pool with retry logic
func NewWithRetry(ctx context.Context, connStr string, callbacks ...ConnConfigCallback) (PgxPoolIface, error) {
	config := retry.PostgreSQLDefaults()

	var pool PgxPoolIface
	err := retry.WithOperation(ctx, config, func() error {
		var attemptErr error
		pool, attemptErr = New(ctx, connStr, callbacks...)
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
	}, "Postgres connect")

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
