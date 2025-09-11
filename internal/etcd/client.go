// Package etcd provides etcd client operations for PostgreSQL synchronization.
package etcd

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
	client *clientv3.Client
	dsn    string
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
		client: client,
		dsn:    dsn,
	}, nil
}

// Close closes the etcd client connection
func (c *EtcdClient) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// Client returns the underlying etcd client for direct access
func (c *EtcdClient) Client() *clientv3.Client {
	return c.client
}

// WatchPrefix sets up a watch for all keys with the given prefix
func (c *EtcdClient) WatchPrefix(ctx context.Context, prefix string, startRevision int64) clientv3.WatchChan {
	opts := []clientv3.OpOption{clientv3.WithPrefix()}
	if startRevision > 0 {
		opts = append(opts, clientv3.WithRev(startRevision+1))
	}

	watchChan := c.client.Watch(ctx, prefix, opts...)
	logrus.WithFields(logrus.Fields{
		"prefix":   prefix,
		"revision": startRevision,
	}).Info("Started etcd watch")

	return watchChan
}

// GetAllKeys retrieves all key-value pairs with the given prefix for initial sync
func (c *EtcdClient) GetAllKeys(ctx context.Context, prefix string) ([]KeyValuePair, error) {
	resp, err := c.client.Get(ctx, prefix, clientv3.WithPrefix(), clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend))
	if err != nil {
		return nil, fmt.Errorf("failed to get all keys: %w", err)
	}

	pairs := make([]KeyValuePair, len(resp.Kvs))
	for i, kv := range resp.Kvs {
		value := string(kv.Value)
		pairs[i] = KeyValuePair{
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
	resp, err := c.client.Put(ctx, key, value)
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
	resp, err := c.client.Delete(ctx, key)
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
func (c *EtcdClient) Get(ctx context.Context, key string) (*KeyValuePair, error) {
	resp, err := c.client.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	if len(resp.Kvs) == 0 {
		return nil, nil // Key not found
	}

	kv := resp.Kvs[0]
	value := string(kv.Value)

	return &KeyValuePair{
		Key:       string(kv.Key),
		Value:     value,
		Revision:  kv.ModRevision,
		Tombstone: false,
	}, nil
}

// KeyValuePair represents a key-value pair from etcd
type KeyValuePair struct {
	Key       string
	Value     string // nullable for tombstones
	Revision  int64
	Tombstone bool
}

// parseEtcdDSN parses etcd DSN format: etcd://host1:port1[,host2:port2]/[prefix]?param=value
func parseEtcdDSN(dsn string) (*clientv3.Config, error) {
	if dsn == "" {
		// Use default etcd configuration
		return &clientv3.Config{
			Endpoints:   []string{"127.0.0.1:2379"},
			DialTimeout: 5 * time.Second,
		}, nil
	}

	// Parse the DSN if provided
	if !strings.HasPrefix(dsn, "etcd://") {
		return nil, fmt.Errorf("etcd DSN must start with etcd://")
	}

	// Remove etcd:// prefix
	dsn = strings.TrimPrefix(dsn, "etcd://")

	// Parse as URL to handle query parameters
	u, err := url.Parse("dummy://" + dsn)
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
