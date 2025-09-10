# Data Model: Bidirectional Synchronization Between etcd and PostgreSQL

## Core Entities

### KeyValueRecord
**Purpose**: Represents a single key-value pair with revision metadata for synchronization

**Fields**:
- `key` (string, required): The key identifier
- `value` (string, nullable): The value content (null for deletions)
- `revision` (int64, required): etcd revision number for ordering and conflict resolution
- `timestamp` (timestamp, required): When the record was created/modified
- `tombstone` (boolean, default false): Indicates if this is a deletion marker

**Validation Rules**:
- Key must not be empty
- Revision must be positive integer
- Tombstone records have null value
- Primary key is composite: (key, revision)

**State Transitions**:
- Create: new key with revision 1
- Update: same key with higher revision
- Delete: tombstone=true with higher revision

### WriteAheadLogEntry
**Purpose**: Tracks pending changes that need to be synchronized to etcd

**Fields**:
- `key` (string, required): The key being modified
- `value` (string, nullable): The new value (null for deletions)
- `revision` (int64, nullable): The current revision before modification (null if key doesn't exist)
- `timestamp` (timestamp, required): When the change was initiated
- `status` (enum): pending, success, conflict, failed
- `retry_count` (int, default 0): Number of retry attempts
- `error_message` (string, nullable): Last error encountered

**Validation Rules**:
- Key must not be empty
- Status must be valid enum value
- Retry count must be non-negative
- Error message required when status is failed

**State Transitions**:
- pending → success: Change successfully applied to etcd
- pending → conflict: etcd has newer revision
- pending → failed: Permanent failure after retries
- conflict/failed → pending: Manual retry triggered

### SynchronizationEvent
**Purpose**: Audit log of synchronization operations for monitoring and debugging

**Fields**:
- `id` (uuid, primary key): Unique event identifier
- `operation` (enum): create, update, delete, conflict, retry, compaction
- `key` (string, required): The key involved in the operation
- `source_revision` (int64, nullable): Source system revision
- `target_revision` (int64, nullable): Target system revision after sync
- `source_system` (enum): etcd, postgresql
- `target_system` (enum): etcd, postgresql
- `status` (enum): success, failure, partial
- `timestamp` (timestamp, required): When the event occurred
- `duration_ms` (int): How long the operation took
- `error_details` (json, nullable): Structured error information

**Validation Rules**:
- Operation must be valid enum value
- Source and target systems must be different
- Duration must be non-negative
- Error details required when status is failure

## Database Schema

### PostgreSQL Tables

```sql
-- Main key-value storage with full revision history
CREATE TABLE etcd (
    ts timestamp with time zone NOT NULL DEFAULT now(),
    key text NOT NULL,
    value text,
    revision bigint NOT NULL,
    tombstone boolean NOT NULL DEFAULT false,
    PRIMARY KEY(key, revision)
);

-- Write-ahead log for tracking changes to be synchronized
CREATE TABLE etcd_wal (
    id serial PRIMARY KEY,
    ts timestamp with time zone NOT NULL DEFAULT now(),
    key text NOT NULL,
    value text,
    revision bigint, -- Current revision before modification
    status text NOT NULL DEFAULT 'pending',
    retry_count integer NOT NULL DEFAULT 0,
    error_message text
);

-- Audit log for synchronization events
CREATE TABLE sync_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    operation text NOT NULL,
    key text NOT NULL,
    source_revision bigint,
    target_revision bigint,
    source_system text NOT NULL,
    target_system text NOT NULL,
    status text NOT NULL,
    timestamp timestamp with time zone NOT NULL DEFAULT now(),
    duration_ms integer,
    error_details jsonb
);
```

### Indexes

```sql
-- Performance indexes
CREATE INDEX idx_etcd_key_revision ON etcd(key, revision DESC);
CREATE INDEX idx_etcd_wal_status ON etcd_wal(status) WHERE status = 'pending';
CREATE INDEX idx_sync_events_timestamp ON sync_events(timestamp);
CREATE INDEX idx_sync_events_key ON sync_events(key);
```

### Triggers

```sql
-- Trigger function to notify on WAL changes
CREATE OR REPLACE FUNCTION notify_etcd_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('etcd_changes', 
        json_build_object(
            'id', NEW.id,
            'key', NEW.key,
            'value', NEW.value,
            'revision', NEW.revision,
            'operation', TG_OP
        )::text
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger on WAL table for real-time notifications
CREATE TRIGGER etcd_wal_notify
    AFTER INSERT OR UPDATE ON etcd_wal
    FOR EACH ROW
    EXECUTE FUNCTION notify_etcd_change();
```

## Entity Relationships

```
KeyValueRecord 1:N ← revision history ← SynchronizationEvent
WriteAheadLogEntry 1:1 ← pending change ← KeyValueRecord
SynchronizationEvent N:1 → operation type → OperationType
```

**Relationship Rules**:
- Each key can have multiple revisions (versioned history)
- Each WAL entry corresponds to one key modification attempt
- Each sync event tracks one operation on one key
- WAL entries are cleaned up after successful synchronization

## Data Flow

### PostgreSQL → etcd Flow
1. Application modifies etcd table
2. Trigger creates WAL entry with current revision
3. NOTIFY sent to sync service
4. Sync service reads WAL entry
5. Service applies change to etcd with revision check
6. WAL entry updated with result (success/conflict/failure)

### etcd → PostgreSQL Flow
1. etcd watch detects change
2. Service reads new revision from etcd
3. Service inserts new record into etcd table
4. Sync event logged for audit

### Conflict Resolution
1. Compare WAL revision with current etcd revision
2. If etcd revision > WAL revision: conflict detected
3. Log conflict event
4. Update WAL status to 'conflict'
5. Operator decides: retry with new revision or discard

## Error Handling

### Transient Errors
- Network timeouts: retry with exponential backoff
- Database connection errors: reconnect and retry
- etcd server unavailable: wait and retry

### Permanent Errors
- Schema validation failures: log and mark as failed
- Permission errors: log and alert operator
- Data corruption: log detailed error and halt sync

### Recovery Scenarios
- Service restart: resume from pending WAL entries
- etcd compaction: detect and trigger full resync
- PostgreSQL failover: reconnect and resume operations
