// Package sync provides synchronization testing for etcd_fdw.
package sync

import (
	"context"
	"testing"
	"time"
)

// TestRetryConfig tests retry configuration
func TestRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()

	if config.MaxRetries != 3 {
		t.Errorf("Expected MaxRetries=3, got %d", config.MaxRetries)
	}
	if config.BaseDelay != 100*time.Millisecond {
		t.Errorf("Expected BaseDelay=100ms, got %v", config.BaseDelay)
	}
	if config.MaxDelay != 5*time.Second {
		t.Errorf("Expected MaxDelay=5s, got %v", config.MaxDelay)
	}
}

// TestRetryWithBackoff tests retry logic
func TestRetryWithBackoff(t *testing.T) {
	ctx := context.Background()
	config := RetryConfig{
		MaxRetries: 2,
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   10 * time.Millisecond,
	}

	// Test successful operation
	attempts := 0
	err := RetryWithBackoff(ctx, config, func() error {
		attempts++
		return nil
	})

	if err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}
	if attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", attempts)
	}
}
