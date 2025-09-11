// Package retry provides common retry logic with exponential backoff for etcd_fdw.
package retry

import (
	"context"
	"time"

	"github.com/sethvargo/go-retry"
	"github.com/sirupsen/logrus"
)

// Config holds configuration for retry logic
type Config struct {
	MaxAttempts   uint64
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	JitterPercent uint64
}

// PostgreSQLDefaults returns sensible defaults for PostgreSQL operations
func PostgreSQLDefaults() *Config {
	return &Config{
		MaxAttempts:   10,
		BaseDelay:     100 * time.Millisecond,
		MaxDelay:      30 * time.Second,
		JitterPercent: 10,
	}
}

// EtcdDefaults returns sensible defaults for etcd operations
func EtcdDefaults() *Config {
	return &Config{
		MaxAttempts:   15, // etcd can take longer to recover
		BaseDelay:     200 * time.Millisecond,
		MaxDelay:      1 * time.Minute,
		JitterPercent: 15, // Higher jitter for etcd
	}
}

// WithOperation performs a general operation with retry logic
func WithOperation(ctx context.Context, config *Config, operation func() error, operationName string) error {
	backoff := config.CreateBackoff()
	return retry.Do(ctx, backoff, func(ctx context.Context) error {
		err := operation()
		if err != nil {
			logrus.WithError(err).
				WithField("operation", operationName).
				Warn("Operation failed, retrying...")
			return retry.RetryableError(err)
		}
		return nil
	})
}

// CreateBackoff creates a reusable backoff strategy from config
func (c *Config) CreateBackoff() retry.Backoff {
	backoff := retry.NewExponential(c.BaseDelay)
	backoff = retry.WithMaxRetries(c.MaxAttempts, backoff)
	backoff = retry.WithCappedDuration(c.MaxDelay, backoff)
	backoff = retry.WithJitterPercent(c.JitterPercent, backoff)
	return backoff
}
