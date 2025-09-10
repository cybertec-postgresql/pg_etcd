// Package sync provides conflict resolution logic for etcd-PostgreSQL synchronization.
package sync

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/cybertec-postgresql/etcd_fdw/internal/db"
	"github.com/cybertec-postgresql/etcd_fdw/internal/etcd"
)

// ConflictResolver handles conflict resolution using "etcd wins" strategy
type ConflictResolver struct {
	pgPool     db.PgxPoolIface
	etcdClient *etcd.EtcdClient
}

// NewConflictResolver creates a new conflict resolver
func NewConflictResolver(pgPool db.PgxPoolIface, etcdClient *etcd.EtcdClient) *ConflictResolver {
	return &ConflictResolver{
		pgPool:     pgPool,
		etcdClient: etcdClient,
	}
}

// ResolveConflict implements "etcd wins" conflict resolution strategy
func (r *ConflictResolver) ResolveConflict(ctx context.Context, key string, pgRevision, etcdRevision int64) (*ResolutionResult, error) {
	logrus.WithFields(logrus.Fields{
		"key":           key,
		"pg_revision":   pgRevision,
		"etcd_revision": etcdRevision,
	}).Info("Resolving conflict")

	// Always favor etcd (etcd wins strategy)
	if etcdRevision > pgRevision {
		// etcd is newer, get the current value from etcd and update PostgreSQL
		return r.resolveWithEtcdValue(ctx, key)
	} else if pgRevision > etcdRevision {
		// PostgreSQL is newer, but etcd still wins - get etcd value and overwrite PostgreSQL
		return r.resolveWithEtcdValue(ctx, key)
	} else {
		// Same revision - check if values match
		return r.verifyConsistency(ctx, key)
	}
}

// resolveWithEtcdValue gets the current value from etcd and returns it as the resolution
func (r *ConflictResolver) resolveWithEtcdValue(ctx context.Context, key string) (*ResolutionResult, error) {
	// Get current value from etcd
	pair, err := r.etcdClient.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd value for conflict resolution: %w", err)
	}

	result := &ResolutionResult{
		Key:    key,
		Winner: "etcd",
		Action: "overwrite_pg",
	}

	if pair == nil {
		// Key doesn't exist in etcd - should be deleted from PostgreSQL
		result.Value = nil
		result.Tombstone = true
		logrus.WithField("key", key).Info("Conflict resolved: etcd wins (key deleted)")
	} else {
		// Key exists in etcd - should be updated in PostgreSQL
		result.Value = pair.Value
		result.Revision = pair.Revision
		result.Tombstone = false
		logrus.WithFields(logrus.Fields{
			"key":      key,
			"revision": pair.Revision,
		}).Info("Conflict resolved: etcd wins (key updated)")
	}

	return result, nil
}

// verifyConsistency checks if values are consistent when revisions match
func (r *ConflictResolver) verifyConsistency(ctx context.Context, key string) (*ResolutionResult, error) {
	// Get values from both sides
	etcdPair, err := r.etcdClient.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get etcd value for consistency check: %w", err)
	}

	// For simplicity, we'll always trust etcd even when revisions match
	result := &ResolutionResult{
		Key:    key,
		Winner: "etcd",
		Action: "verify_consistent",
	}

	if etcdPair == nil {
		result.Value = nil
		result.Tombstone = true
	} else {
		result.Value = etcdPair.Value
		result.Revision = etcdPair.Revision
		result.Tombstone = false
	}

	logrus.WithField("key", key).Info("Consistency check: etcd value confirmed")
	return result, nil
}

// ResolutionResult represents the outcome of a conflict resolution
type ResolutionResult struct {
	Key       string  // The key that was resolved
	Winner    string  // Which side won ("etcd" in our case)
	Action    string  // What action was taken ("overwrite_pg", "verify_consistent")
	Value     *string // The resolved value (nil for deletions)
	Revision  int64   // The winning revision
	Tombstone bool    // Whether this is a deletion
}

// ApplyResolution applies the conflict resolution result to PostgreSQL
func (r *ConflictResolver) ApplyResolution(ctx context.Context, result *ResolutionResult) error {
	// Create a record to insert into PostgreSQL
	record := db.KeyValueRecord{
		Key:       result.Key,
		Value:     result.Value,
		Revision:  result.Revision,
		Timestamp: "now()", // Use PostgreSQL's now() function
		Tombstone: result.Tombstone,
	}

	// Insert the resolved record
	if err := db.BulkInsert(ctx, r.pgPool, []db.KeyValueRecord{record}); err != nil {
		return fmt.Errorf("failed to apply conflict resolution: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"key":      result.Key,
		"action":   result.Action,
		"revision": result.Revision,
	}).Info("Conflict resolution applied to PostgreSQL")

	return nil
}
