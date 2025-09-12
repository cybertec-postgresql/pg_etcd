// Package sync provides synchronization orchestration between etcd and PostgreSQL.
package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const InvalidRevision = -1

// Service orchestrates bidirectional synchronization between etcd and PostgreSQL
type Service struct {
	pgPool          PgxIface
	etcdClient      *EtcdClient
	prefix          string
	pollingInterval time.Duration
}

// NewService creates a new synchronization service
func NewService(pgPool PgxIface, etcdClient *EtcdClient, pollingInterval time.Duration) *Service {
	return &Service{
		pgPool:          pgPool,
		etcdClient:      etcdClient,
		pollingInterval: pollingInterval,
	}
}

// Start begins the bidirectional synchronization process
func (s *Service) Start(ctx context.Context) error {
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
	records := make([]KeyValueRecord, len(pairs))
	for i, pair := range pairs {
		records[i] = KeyValueRecord{
			Key:       pair.Key,
			Value:     pair.Value,
			Revision:  pair.Revision,
			Ts:        time.Now(),
			Tombstone: pair.Tombstone,
		}
	}

	// Bulk insert using COPY
	if err := BulkInsert(ctx, s.pgPool, records); err != nil {
		return fmt.Errorf("failed to bulk insert records: %w", err)
	}

	logrus.WithField("count", len(records)).Info("Initial sync completed successfully")
	return nil
}

// syncEtcdToPostgreSQL continuously watches etcd for changes and syncs to PostgreSQL
func (s *Service) syncEtcdToPostgreSQL(ctx context.Context) error {
	logrus.Info("Starting etcd to PostgreSQL sync watcher")

	// Get the latest revision from PostgreSQL to resume from
	latestRevision, err := GetLatestRevision(ctx, s.pgPool)
	if err != nil {
		return fmt.Errorf("failed to get latest revision: %w", err)
	}

	// Start watching from the next revision with automatic recovery
	watchChan := s.etcdClient.WatchWithRecovery(ctx, latestRevision)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case watchResp, ok := <-watchChan:
			if !ok {
				// Watch channel closed, likely due to context cancellation
				return ctx.Err()
			}

			if watchResp.Canceled {
				// This should be handled by WatchWithRecovery, but log it
				logrus.Warn("etcd watch was canceled - recovery should be automatic")
				continue
			}

			if err := watchResp.Err(); err != nil {
				logrus.WithError(err).Error("etcd watch error - recovery should be automatic")
				continue
			}

			// Process all events in this watch response
			for _, event := range watchResp.Events {
				err := RetryWithBackoff(ctx, DefaultRetryConfig(), func() error {
					return s.processEtcdEvent(ctx, event)
				})

				if err != nil {
					logrus.WithError(err).WithField("key", string(event.Kv.Key)).Error("Failed to process etcd event after retries")
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

	var record KeyValueRecord
	record.Key = key
	record.Revision = revision
	record.Ts = time.Now()

	switch event.Type {
	case clientv3.EventTypePut:
		value := string(event.Kv.Value)
		record.Value = value
		record.Tombstone = false
		logrus.WithFields(logrus.Fields{
			"key":      key,
			"revision": revision,
			"type":     "PUT",
		}).Debug("Processing etcd PUT event")

	case clientv3.EventTypeDelete:
		record.Value = ""
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
	if err := BulkInsert(ctx, s.pgPool, []KeyValueRecord{record}); err != nil {
		return fmt.Errorf("failed to insert event into PostgreSQL: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"key":      key,
		"revision": revision,
		"type":     event.Type.String(),
	}).Info("Synced etcd event to PostgreSQL")

	return nil
}

// syncPostgreSQLToEtcd polls for pending records and syncs them to etcd
func (s *Service) syncPostgreSQLToEtcd(ctx context.Context) error {
	logrus.Info("Starting PostgreSQL to etcd sync poller with polling mechanism")

	ticker := time.NewTicker(s.pollingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.pollAndProcessPendingRecords(ctx); err != nil {
				logrus.WithError(err).Error("Failed to poll and process pending records")
			}
		}
	}
}

func (s *Service) pollAndProcessPendingRecords(ctx context.Context) error {
	// Get pending records (revision = -1) using SELECT FOR UPDATE SKIP LOCKED
	pendingRecords, err := GetPendingRecords(ctx, s.pgPool)
	if err != nil {
		return fmt.Errorf("failed to get pending records: %w", err)
	}

	if len(pendingRecords) == 0 {
		return nil // No pending records to process
	}

	logrus.WithField("count", len(pendingRecords)).Debug("Found pending records to sync to etcd")

	// Process each pending record with retry logic
	for _, record := range pendingRecords {
		err := RetryWithBackoff(ctx, DefaultRetryConfig(), func() error {
			return s.processPendingRecord(ctx, record)
		})

		if err != nil {
			logrus.WithError(err).WithField("key", record.Key).Error("Failed to process pending record after retries")
			// Continue processing other records rather than failing entirely
		}
	}

	return nil
}

// processPendingRecord processes a single pending record and syncs it to etcd
func (s *Service) processPendingRecord(ctx context.Context, record KeyValueRecord) error {
	logrus.WithFields(logrus.Fields{
		"key":       record.Key,
		"tombstone": record.Tombstone,
	}).Debug("Processing pending record")

	// Apply the change to etcd with retry logic
	var newRevision int64
	if record.Tombstone {
		// Delete operation
		err := RetryEtcdOperation(ctx, func() error {
			resp, delErr := s.etcdClient.Delete(ctx, record.Key)
			if delErr != nil {
				return delErr
			}
			newRevision = resp.Header.Revision
			return nil
		}, "etcd_delete")

		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"key":       record.Key,
				"operation": "etcd_delete",
			}).Error("Failed to sync delete to etcd after retries")
			return fmt.Errorf("failed to delete key from etcd: %w", err)
		}

		logrus.WithFields(logrus.Fields{
			"key":      record.Key,
			"revision": newRevision,
		}).Info("Synced PostgreSQL change to etcd (DELETE)")
	} else {
		// Put operation
		err := RetryEtcdOperation(ctx, func() error {
			resp, putErr := s.etcdClient.Put(ctx, record.Key, record.Value)
			if putErr != nil {
				return putErr
			}
			newRevision = resp.Header.Revision
			return nil
		}, "etcd_put")

		if err != nil {
			logrus.WithError(err).WithFields(logrus.Fields{
				"key":       record.Key,
				"operation": "etcd_put",
			}).Error("Failed to sync put to etcd after retries")
			return fmt.Errorf("failed to put key to etcd: %w", err)
		}

		logrus.WithFields(logrus.Fields{
			"key":      record.Key,
			"revision": newRevision,
		}).Info("Synced PostgreSQL change to etcd (PUT)")
	}

	// Update local record with the new etcd revision
	return UpdateRevision(ctx, s.pgPool, record.Key, newRevision)
}
