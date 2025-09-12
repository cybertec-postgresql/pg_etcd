// Package sync provides consolidated etcd client operations for PostgreSQL synchronization.
package sync

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// EtcdClient handles all etcd operations for PostgreSQL synchronization
type EtcdClient struct {
	*clientv3.Client
}

// NewEtcdClient creates a new etcd client with DSN parsing
func NewEtcdClient(dsn string) (*EtcdClient, error) {
	config, err := parseEtcdDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse etcd DSN: %w", err)
	}

	client, err := clientv3.New(*config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to etcd: %w", err)
	}

	logrus.WithField("endpoints", config.Endpoints).Info("Connected to etcd successfully")

	return &EtcdClient{
		Client: client,
	}, nil
}

// Close closes the etcd client connection
func (c *EtcdClient) Close() error {
	if c.Client != nil {
		return c.Client.Close()
	}
	return nil
}

// WatchPrefix sets up a watch for all keys with the given prefix
func (c *EtcdClient) WatchPrefix(ctx context.Context, prefix string, startRevision int64) clientv3.WatchChan {
	opts := []clientv3.OpOption{clientv3.WithPrefix()}
	if startRevision > 0 {
		opts = append(opts, clientv3.WithRev(startRevision+1))
	}

	watchChan := c.Client.Watch(ctx, prefix, opts...)
	logrus.WithFields(logrus.Fields{
		"prefix":   prefix,
		"revision": startRevision,
	}).Info("Started etcd watch")

	return watchChan
}

// GetAllKeys retrieves all key-value pairs with the given prefix for initial sync
func (c *EtcdClient) GetAllKeys(ctx context.Context, prefix string) ([]KeyValueRecord, error) {
	resp, err := c.Client.Get(ctx, prefix, clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	if err != nil {
		return nil, fmt.Errorf("failed to get all keys: %w", err)
	}

	pairs := make([]KeyValueRecord, len(resp.Kvs))
	for i, kv := range resp.Kvs {
		value := string(kv.Value)
		pairs[i] = KeyValueRecord{
			Key:       string(kv.Key),
			Value:     value,
			Revision:  kv.ModRevision,
			Tombstone: false,
		}
	}

	logrus.WithFields(logrus.Fields{
		"prefix":          prefix,
		"count":           len(pairs),
		"header_revision": resp.Header.Revision,
	}).Info("Retrieved all keys from etcd")

	return pairs, nil
}

// Put stores a key-value pair in etcd
func (c *EtcdClient) Put(ctx context.Context, key, value string) (*clientv3.PutResponse, error) {
	resp, err := c.Client.Put(ctx, key, value)
	if err != nil {
		return nil, fmt.Errorf("failed to put key %s: %w", key, err)
	}

	logrus.WithFields(logrus.Fields{
		"key":      key,
		"revision": resp.Header.Revision,
	}).Debug("Put key to etcd")

	return resp, nil
}

// Delete removes a key from etcd
func (c *EtcdClient) Delete(ctx context.Context, key string) (*clientv3.DeleteResponse, error) {
	resp, err := c.Client.Delete(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to delete key %s: %w", key, err)
	}

	logrus.WithFields(logrus.Fields{
		"key":      key,
		"revision": resp.Header.Revision,
		"deleted":  resp.Deleted,
	}).Debug("Deleted key from etcd")

	return resp, nil
}

// Get retrieves a single key from etcd
func (c *EtcdClient) Get(ctx context.Context, key string) (*KeyValueRecord, error) {
	resp, err := c.Client.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	if len(resp.Kvs) == 0 {
		return nil, nil // Key not found
	}

	kv := resp.Kvs[0]
	value := string(kv.Value)

	return &KeyValueRecord{
		Key:       string(kv.Key),
		Value:     value,
		Revision:  kv.ModRevision,
		Tombstone: false,
	}, nil
}

// NewEtcdClientWithRetry creates a new etcd client with retry logic
func NewEtcdClientWithRetry(ctx context.Context, dsn string) (*EtcdClient, error) {
	config := DefaultRetryConfig()

	var client *EtcdClient
	err := RetryWithBackoff(ctx, config, func() error {
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
	})

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
	config := DefaultRetryConfig()
	return RetryWithBackoff(ctx, config, operation)
}

// parseEtcdDSN parses etcd DSN format: etcd://[user:password@]host1:port1[,host2:port2]/[prefix]?param=value
func parseEtcdDSN(dsn string) (*clientv3.Config, error) {
	if dsn == "" {
		return nil, fmt.Errorf("etcd DSN is required")
	}

	// Parse the DSN if provided
	if !strings.HasPrefix(dsn, "etcd://") {
		return nil, fmt.Errorf("etcd DSN must start with etcd://")
	}

	// Parse as proper URL
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSN: %w", err)
	}

	// Extract endpoints from host part
	endpoints := strings.Split(u.Host, ",")
	for i, endpoint := range endpoints {
		if !strings.Contains(endpoint, ":") {
			endpoints[i] = endpoint + ":2379" // Default etcd port
		}
	}

	config := &clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	}

	// Extract username and password if provided
	if u.User != nil {
		username := u.User.Username()
		password, _ := u.User.Password()
		if username != "" {
			config.Username = username
		}
		if password != "" {
			config.Password = password
		}
	}

	// Parse query parameters
	params := u.Query()

	if timeout := params.Get("dial_timeout"); timeout != "" {
		if d, err := time.ParseDuration(timeout); err == nil {
			config.DialTimeout = d
		}
	}

	if timeout := params.Get("request_timeout"); timeout != "" {
		// Note: clientv3.Config doesn't have a global RequestTimeout
		// This would need to be handled per-request using context
		logrus.WithField("request_timeout", timeout).Debug("Request timeout parameter noted")
	}

	if username := params.Get("username"); username != "" {
		config.Username = username
	}

	if password := params.Get("password"); password != "" {
		config.Password = password
	}

	if tlsParam := params.Get("tls"); tlsParam == "enabled" {
		// Basic TLS config - in production this should be more sophisticated
		config.TLS = &tls.Config{
			InsecureSkipVerify: true, // For development - should be configurable
		}
	}

	return config, nil
}

// GetPrefix extracts the prefix from the etcd DSN path
func GetPrefix(dsn string) string {
	if dsn == "" || !strings.HasPrefix(dsn, "etcd://") {
		return "/"
	}

	// Parse as URL to extract path
	u, err := url.Parse(dsn)
	if err != nil {
		return "/"
	}

	if u.Path == "" {
		return "/"
	}

	return u.Path
}
