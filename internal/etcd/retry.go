// Package etcd provides connection retry and recovery logic for etcd clients.
package etcd

import (
	"context"
	"time"

	"github.com/cybertec-postgresql/etcd_fdw/internal/retry"
	"github.com/sirupsen/logrus"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// NewEtcdClientWithRetry creates a new etcd client with retry logic
func NewEtcdClientWithRetry(ctx context.Context, dsn string) (*EtcdClient, error) {
	config := retry.EtcdDefaults()

	var client *EtcdClient
	err := retry.WithOperation(ctx, config, func() error {
		var attemptErr error
		client, attemptErr = NewEtcdClient(dsn)
		if attemptErr != nil {
			return attemptErr
		}

		// Test the connection
		if _, testErr := client.Get(ctx, "healthcheck"); testErr != nil {
			if client != nil {
				client.Close()
			}
			return testErr
		}

		return nil
	}, "etcd connect")

	if err != nil {
		logrus.WithError(err).Error("Failed to establish etcd connection after all retries")
		return nil, err
	}

	return client, nil
}

// WatchWithRecovery wraps the etcd watch functionality with automatic recovery
func (c *EtcdClient) WatchWithRecovery(ctx context.Context, prefix string, startRevision int64) <-chan clientv3.WatchResponse {
	watchChan := make(chan clientv3.WatchResponse)

	go func() {
		defer close(watchChan)

		currentRevision := startRevision

		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Attempt to establish watch
				innerWatchChan := c.WatchPrefix(ctx, prefix, currentRevision)

				for {
					select {
					case <-ctx.Done():
						return
					case watchResp, ok := <-innerWatchChan:
						if !ok {
							// Channel closed, need to restart
							logrus.Warn("etcd watch channel closed, attempting to restart")
							break
						}

						if watchResp.Canceled {
							logrus.Warn("etcd watch was canceled, attempting to restart")
							break
						}

						if err := watchResp.Err(); err != nil {
							logrus.WithError(err).Error("etcd watch error, attempting to restart")
							break
						}

						// Update revision from successful events
						for _, event := range watchResp.Events {
							if event.Kv.ModRevision > currentRevision {
								currentRevision = event.Kv.ModRevision
							}
						}

						// Forward the response
						select {
						case watchChan <- watchResp:
						case <-ctx.Done():
							return
						}

						continue // Continue with current watch
					}

					// If we reach here, we need to restart the watch
					break
				}

				logrus.WithField("revision", currentRevision).Info("Restarting etcd watch")
				time.Sleep(time.Second) // Simple delay before restart
			}
		}
	}()

	return watchChan
}

// RetryEtcdOperation retries an etcd operation with exponential backoff
func RetryEtcdOperation(ctx context.Context, operation func() error, operationName string) error {
	config := retry.EtcdDefaults()
	return retry.WithOperation(ctx, config, operation, operationName)
}
