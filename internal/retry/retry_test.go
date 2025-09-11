package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPostgreSQLDefaults(t *testing.T) {
	config := PostgreSQLDefaults()
	if config.MaxAttempts != 10 {
		t.Errorf("Expected MaxAttempts=10, got %d", config.MaxAttempts)
	}
	if config.BaseDelay != 100*time.Millisecond {
		t.Errorf("Expected BaseDelay=100ms, got %v", config.BaseDelay)
	}
	if config.MaxDelay != 30*time.Second {
		t.Errorf("Expected MaxDelay=30s, got %v", config.MaxDelay)
	}
	if config.JitterPercent != 10 {
		t.Errorf("Expected JitterPercent=10, got %d", config.JitterPercent)
	}
}

func TestEtcdDefaults(t *testing.T) {
	config := EtcdDefaults()
	if config.MaxAttempts != 15 {
		t.Errorf("Expected MaxAttempts=15, got %d", config.MaxAttempts)
	}
	if config.BaseDelay != 200*time.Millisecond {
		t.Errorf("Expected BaseDelay=200ms, got %v", config.BaseDelay)
	}
	if config.MaxDelay != 1*time.Minute {
		t.Errorf("Expected MaxDelay=1m, got %v", config.MaxDelay)
	}
	if config.JitterPercent != 15 {
		t.Errorf("Expected JitterPercent=15, got %d", config.JitterPercent)
	}
}

func TestWithOperation_Success(t *testing.T) {
	config := &Config{
		MaxAttempts:   3,
		BaseDelay:     1 * time.Millisecond,
		MaxDelay:      10 * time.Millisecond,
		JitterPercent: 10,
	}

	callCount := 0
	operation := func() error {
		callCount++
		return nil
	}

	ctx := context.Background()
	err := WithOperation(ctx, config, operation, "test-operation")

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	if callCount != 1 {
		t.Errorf("Expected operation to be called once, got %d", callCount)
	}
}

func TestWithOperation_ExceedsMaxAttempts(t *testing.T) {
	config := &Config{
		MaxAttempts:   3,
		BaseDelay:     1 * time.Millisecond,
		MaxDelay:      10 * time.Millisecond,
		JitterPercent: 10,
	}

	callCount := 0
	operation := func() error {
		callCount++
		return errors.New("persistent failure")
	}

	ctx := context.Background()
	err := WithOperation(ctx, config, operation, "test-operation")

	if err == nil {
		t.Error("Expected an error, got nil")
	}
	// go-retry does MaxAttempts + 1 total attempts (initial + retries)
	if callCount != 4 {
		t.Errorf("Expected operation to be called 4 times (initial + 3 retries), got %d", callCount)
	}
}

func TestCreateBackoff(t *testing.T) {
	config := &Config{
		MaxAttempts:   5,
		BaseDelay:     100 * time.Millisecond,
		MaxDelay:      5 * time.Second,
		JitterPercent: 20,
	}

	backoff := config.CreateBackoff()
	if backoff == nil {
		t.Error("Expected backoff to be created, got nil")
	}
}
