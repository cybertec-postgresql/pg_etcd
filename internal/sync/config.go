// Package sync provides unified connection management for both etcd and PostgreSQL.
package sync

import (
	"time"
)

// Config represents the main application configuration from command line
type Config struct {
	PostgresDSN     string
	EtcdDSN         string
	LogLevel        string
	PollingInterval time.Duration
}

// KeyValueRecord represents a unified key-value record used throughout the system
// It handles both etcd data and PostgreSQL table records
type KeyValueRecord struct {
	Key       string
	Value     string // nullable for tombstones in database, empty string in code
	Revision  int64  // -1 for pending sync to etcd, >0 for real etcd revision
	Ts        time.Time
	Tombstone bool
}
