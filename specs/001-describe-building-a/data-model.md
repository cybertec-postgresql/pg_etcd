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
- `timestamp` (timestamp, required): When the change was initiated (PRIMARY KEY with key)

**Validation Rules**:

- Key must not be empty
- Timestamp and key combination must be unique
- Primary key is composite: (key, ts)

**State Transitions**:

- Entry created → processed by sync service → revision updated with the value from etcd successful sync
- Failures handled by sync service retry logic
- After unsuccessful retries for a configurable number of attempts, entry has revision set to -1 (failed state) and is logged for manual review

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
    ts timestamp with time zone NOT NULL DEFAULT now(),
    key text NOT NULL,
    value text,
    revision bigint, -- Current revision before modification, if null = new key. After sync: -1 = failed, >0 = valid synced from etcd
    PRIMARY KEY(key, ts)
);
```

### Database Functions

```sql
-- Function: Get latest value for a key with revision enforcement
CREATE OR REPLACE FUNCTION etcd_get(p_key text)
RETURNS TABLE(key text, value text, revision bigint, tombstone boolean, ts timestamp with time zone)
LANGUAGE sql STABLE AS $$
    SELECT e.key, e.value, e.revision, e.tombstone, e.ts
    FROM etcd e
    WHERE e.key = p_key
    ORDER BY e.revision DESC
    LIMIT 1;
$$;

-- Function: Get all revisions for a key higher than passed revision
CREATE OR REPLACE FUNCTION etcd_get_all(p_key text, p_min_revision bigint DEFAULT 0)
RETURNS TABLE(key text, value text, revision bigint, tombstone boolean, ts timestamp with time zone)
LANGUAGE sql STABLE AS $$
    SELECT e.key, e.value, e.revision, e.tombstone, e.ts
    FROM etcd e
    WHERE e.key = p_key AND e.revision > p_min_revision
    ORDER BY e.revision ASC;
$$;

-- Function: Set key-value (logs to WAL for synchronization to etcd)
CREATE OR REPLACE FUNCTION etcd_set(p_key text, p_value text)
RETURNS timestamp with time zone
LANGUAGE sql AS $$
    INSERT INTO etcd_wal (key, value, revision)
    SELECT p_key, p_value, (SELECT revision FROM etcd_get(p_key))
    RETURNING ts;
$$;

-- Function: Delete key (logs to WAL for synchronization to etcd)
CREATE OR REPLACE FUNCTION etcd_delete(p_key text)
RETURNS timestamp with time zone
LANGUAGE sql AS $$
    INSERT INTO etcd_wal (key, value, revision)
    SELECT p_key, NULL, (SELECT revision FROM etcd_get(p_key))
    RETURNING ts;
$$;
```

### Indexes

```sql
-- Performance indexes
CREATE INDEX idx_etcd_key_revision ON etcd(key, revision DESC);
CREATE INDEX idx_etcd_wal_key ON etcd_wal(key);
CREATE INDEX idx_etcd_wal_ts ON etcd_wal(ts);
```

### Triggers

```sql
-- Trigger function to notify on WAL changes
CREATE OR REPLACE FUNCTION notify_etcd_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('etcd_changes', 
        json_build_object(
            'key', NEW.key,
            'ts', NEW.ts,
            'value', NEW.value,
            'revision', NEW.revision,
            'operation', CASE 
                WHEN NEW.value IS NULL THEN 'DELETE'
                WHEN NEW.revision IS NULL THEN 'CREATE'
                ELSE 'UPDATE'
            END
        )::text
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Trigger on WAL table for real-time notifications
CREATE TRIGGER etcd_wal_notify
    AFTER INSERT ON etcd_wal
    FOR EACH ROW
    EXECUTE FUNCTION notify_etcd_change();
```

## Entity Relationships

```code
KeyValueRecord 1:N ← revision history
WriteAheadLogEntry 1:1 ← pending change ← KeyValueRecord
```

**Relationship Rules**:

- Each key can have multiple revisions (versioned history)
- Each WAL entry corresponds to one key modification attempt
- WAL entries are processed and revision is set after successful/failed synchronization

## Data Flow

### PostgreSQL → etcd Flow

1. Application calls `etcd_set()` or `etcd_delete()` functions
2. Functions create WAL entry with current revision from `etcd_get()`
3. Trigger fires and sends NOTIFY with JSON payload to sync service
4. Sync service reads notification payload
5. Service applies change to etcd with conflict resolution (etcd wins)
6. WAL entry is updated after processing

### etcd → PostgreSQL Flow

1. etcd watch detects change
2. Service reads new revision from etcd
3. Service inserts new record into etcd table using bulk COPY
4. Revision history maintained in PostgreSQL

### Conflict Resolution

1. Compare WAL revision with current etcd revision
2. If etcd revision > WAL revision: conflict detected
3. Skip local change (etcd wins policy)
4. WAL entry is updated after processing (conflict resolved)
5. Log conflict for monitoring

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
