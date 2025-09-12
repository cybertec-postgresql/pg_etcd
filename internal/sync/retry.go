package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// RetryConfig contains retry configuration parameters
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryConfig provides sensible defaults for retry operations
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   5 * time.Second,
	}
}

// RetryWithBackoff executes a function with exponential backoff retry logic
func RetryWithBackoff(ctx context.Context, config RetryConfig, operation func() error) error {
	var lastErr error
	delay := config.BaseDelay

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		if err := operation(); err != nil {
			lastErr = err
			logrus.WithFields(logrus.Fields{
				"attempt": attempt + 1,
				"error":   err,
				"delay":   delay,
			}).Warn("Operation failed, retrying")

			// Exponential backoff with cap
			delay *= 2
			if delay > config.MaxDelay {
				delay = config.MaxDelay
			}
			continue
		}

		return nil
	}

	return fmt.Errorf("operation failed after %d attempts: %w", config.MaxRetries+1, lastErr)
}
