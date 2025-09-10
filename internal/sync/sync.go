// Package sync provides synchronization orchestration between etcd and PostgreSQL.
package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/cybertec-postgresql/etcd_fdw/internal/db"
	"github.com/cybertec-postgresql/etcd_fdw/internal/etcd"
)

// Service orchestrates bidirectional synchronization between etcd and PostgreSQL
type Service struct {
	pgPool     db.PgxPoolIface
	etcdClient *etcd.EtcdClient
	prefix     string
	dryRun     bool
}

// NewService creates a new synchronization service
func NewService(pgPool db.PgxPoolIface, etcdClient *etcd.EtcdClient, prefix string, dryRun bool) *Service {
	return &Service{
		pgPool:     pgPool,
		etcdClient: etcdClient,
		prefix:     prefix,
		dryRun:     dryRun,
	}
}

// Start begins the bidirectional synchronization process
func (s *Service) Start(ctx context.Context) error {
	if s.dryRun {
		logrus.Info("Dry run mode - would start bidirectional sync")
		return nil
	}

	logrus.Info("Starting etcd_fdw bidirectional synchronization")

	// Perform initial sync from etcd to PostgreSQL
	if err := s.initialSync(ctx); err != nil {
		return fmt.Errorf("initial sync failed: %w", err)
	}

	// Start continuous synchronization in both directions
	errChan := make(chan error, 2)

	// Start etcd to PostgreSQL sync
	go func() {
		errChan <- s.syncEtcdToPostgreSQL(ctx)
	}()

	// Start PostgreSQL to etcd sync
	go func() {
		errChan <- s.syncPostgreSQLToEtcd(ctx)
	}()

	// Wait for either goroutine to error or context cancellation
	select {
	case err := <-errChan:
		return fmt.Errorf("sync error: %w", err)
	case <-ctx.Done():
		logrus.Info("Synchronization stopped due to context cancellation")
		return ctx.Err()
	}
}

// initialSync performs the initial bulk sync from etcd to PostgreSQL
func (s *Service) initialSync(ctx context.Context) error {
	logrus.Info("Starting initial sync from etcd to PostgreSQL")

	// Get all keys from etcd with the specified prefix
	pairs, err := s.etcdClient.GetAllKeys(ctx, s.prefix)
	if err != nil {
		return fmt.Errorf("failed to get all keys from etcd: %w", err)
	}

	if len(pairs) == 0 {
		logrus.Info("No keys found in etcd for initial sync")
		return nil
	}

	// Convert to PostgreSQL records
	records := make([]db.KeyValueRecord, len(pairs))
	for i, pair := range pairs {
		records[i] = db.KeyValueRecord{
			Key:       pair.Key,
			Value:     pair.Value,
			Revision:  pair.Revision,
			Timestamp: time.Now().Format(time.RFC3339),
			Tombstone: pair.Tombstone,
		}
	}

	// Bulk insert using COPY
	if err := db.BulkInsert(ctx, s.pgPool, records); err != nil {
		return fmt.Errorf("failed to bulk insert records: %w", err)
	}

	logrus.WithField("count", len(records)).Info("Initial sync completed successfully")
	return nil
}

// syncEtcdToPostgreSQL continuously watches etcd for changes and syncs to PostgreSQL
func (s *Service) syncEtcdToPostgreSQL(ctx context.Context) error {
	logrus.Info("Starting etcd to PostgreSQL sync watcher")

	// Get the latest revision from PostgreSQL to resume from
	latestRevision, err := db.GetLatestRevision(ctx, s.pgPool)
	if err != nil {
		return fmt.Errorf("failed to get latest revision: %w", err)
	}

	// Start watching from the next revision
	watchChan := s.etcdClient.WatchPrefix(ctx, s.prefix, latestRevision)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case watchResp := <-watchChan:
			if watchResp.Canceled {
				logrus.Warn("etcd watch was canceled, attempting to restart")
				// In a production system, we would implement exponential backoff here
				time.Sleep(time.Second)
				watchChan = s.etcdClient.WatchPrefix(ctx, s.prefix, latestRevision)
				continue
			}

			if err := watchResp.Err(); err != nil {
				logrus.WithError(err).Error("etcd watch error")
				return fmt.Errorf("etcd watch error: %w", err)
			}

			// Process all events in this watch response
			for _, event := range watchResp.Events {
				if err := s.processEtcdEvent(ctx, event); err != nil {
					logrus.WithError(err).WithField("key", string(event.Kv.Key)).Error("Failed to process etcd event")
					// Continue processing other events rather than failing entirely
				} else {
					latestRevision = event.Kv.ModRevision
				}
			}
		}
	}
}

// processEtcdEvent processes a single etcd event and syncs it to PostgreSQL
func (s *Service) processEtcdEvent(ctx context.Context, event *clientv3.Event) error {
	key := string(event.Kv.Key)
	revision := event.Kv.ModRevision

	var record db.KeyValueRecord
	record.Key = key
	record.Revision = revision
	record.Timestamp = time.Now().Format(time.RFC3339)

	switch event.Type {
	case clientv3.EventTypePut:
		value := string(event.Kv.Value)
		record.Value = &value
		record.Tombstone = false
		logrus.WithFields(logrus.Fields{
			"key":      key,
			"revision": revision,
			"type":     "PUT",
		}).Debug("Processing etcd PUT event")

	case clientv3.EventTypeDelete:
		record.Value = nil
		record.Tombstone = true
		logrus.WithFields(logrus.Fields{
			"key":      key,
			"revision": revision,
			"type":     "DELETE",
		}).Debug("Processing etcd DELETE event")

	default:
		return fmt.Errorf("unknown event type: %v", event.Type)
	}

	// Insert the record into PostgreSQL
	if err := db.BulkInsert(ctx, s.pgPool, []db.KeyValueRecord{record}); err != nil {
		return fmt.Errorf("failed to insert event into PostgreSQL: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"key":      key,
		"revision": revision,
		"type":     event.Type.String(),
	}).Info("Synced etcd event to PostgreSQL")

	return nil
}

// syncPostgreSQLToEtcd listens for PostgreSQL WAL notifications and syncs to etcd
func (s *Service) syncPostgreSQLToEtcd(ctx context.Context) error {
	logrus.Info("Starting PostgreSQL to etcd sync listener")

	// Set up LISTEN connection for WAL notifications
	conn, err := db.SetupListen(ctx, s.pgPool, "etcd_changes")
	if err != nil {
		return fmt.Errorf("failed to setup PostgreSQL LISTEN: %w", err)
	}
	defer conn.Close(ctx)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Wait for notification with timeout
			notification, err := conn.WaitForNotification(ctx)
			if err != nil {
				// Check if it's a timeout or context cancellation
				if ctx.Err() != nil {
					return ctx.Err()
				}
				logrus.WithError(err).Warn("PostgreSQL notification wait error")
				continue
			}

			if err := s.processPostgreSQLNotification(ctx, notification); err != nil {
				logrus.WithError(err).WithField("payload", "unknown").Error("Failed to process PostgreSQL notification")
				// Continue processing other notifications rather than failing entirely
			}
		}
	}
}

// processPostgreSQLNotification processes a PostgreSQL NOTIFY and syncs to etcd
func (s *Service) processPostgreSQLNotification(ctx context.Context, notification interface{}) error {
	// In a real implementation, we would parse the JSON payload to get WAL entry details
	// For now, we'll log that we received the notification
	logrus.WithField("notification", notification).Info("Received PostgreSQL notification")

	// TODO: Parse the notification and sync the change to etcd
	// This would involve:
	// 1. Parse notification JSON to get key, value, revision
	// 2. Apply conflict resolution logic
	// 3. Put/Delete to etcd
	// 4. Mark WAL entry as processed

	return nil
}
