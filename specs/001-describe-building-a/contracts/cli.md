# CLI Interface Contract

## etcd_fdw Command Interface

### Main Command
```bash
etcd_fdw [options]
```

### Command Line Options
```
--postgres-dsn, -p     PostgreSQL connection string (required)
                       Format: "postgres://user:password@host:port/database?param=value"

--etcd-dsn, -e         etcd connection string (required)  
                       Format: "etcd://host:port[,host:port]/[prefix]?param=value"
                       Example: "etcd://localhost:2379/service/batman?tls=enabled&dial_timeout=5s"

--log-level, -l        Log level: debug|info|warn|error (default: info)
--help, -h             Show help message
--version, -v          Show version information
--dry-run              Show what would be done without executing
```

### Behavior
The program starts immediately and performs bidirectional synchronization:
1. Connects to PostgreSQL and etcd using provided connection strings
2. Sets up etcd watch for changes
3. Sets up PostgreSQL LISTEN for WAL notifications
4. Performs initial sync from etcd to PostgreSQL using COPY
5. Continuously syncs changes in both directions
6. Outputs structured logs based on log level
7. Runs until interrupted (Ctrl+C)

### Exit Codes
- 0: Normal termination (Ctrl+C)
- 1: Configuration error (invalid connection strings)
- 2: Connection error (PostgreSQL or etcd unreachable)
- 3: Permission error
- 4: Runtime error during sync

### Example Usage
```bash
# Basic usage
etcd_fdw --postgres-dsn "postgres://sync_user:password@localhost:5432/sync_db" \
         --etcd-dsn "etcd://localhost:2379/"

# With specific etcd prefix and TLS
etcd_fdw -p "postgres://user:pass@localhost/db" \
         -e "etcd://localhost:2379/config/?tls=enabled&dial_timeout=5s"

# Debug mode with dry run
etcd_fdw -p "postgres://user:pass@localhost/db" \
         -e "etcd://localhost:2379/" \
         --log-level debug --dry-run
```

### Log Output Examples
```
2025-09-10T15:30:00Z INFO Starting etcd_fdw v1.0.0
2025-09-10T15:30:00Z INFO Connected to PostgreSQL: localhost:5432/sync_db
2025-09-10T15:30:00Z INFO Connected to etcd: localhost:2379
2025-09-10T15:30:00Z INFO Starting initial sync from etcd to PostgreSQL
2025-09-10T15:30:01Z INFO Copied 1247 keys from etcd to PostgreSQL
2025-09-10T15:30:01Z INFO Started etcd watch for prefix: /
2025-09-10T15:30:01Z INFO Started PostgreSQL change listener
2025-09-10T15:30:01Z INFO Synchronization active
2025-09-10T15:30:05Z INFO etcd change: PUT /config/app/timeout = "30s" (rev: 1248)
2025-09-10T15:30:05Z INFO PostgreSQL notification: key=/config/db/host, operation=INSERT
2025-09-10T15:30:05Z WARN Conflict detected for /config/test/key - etcd wins (rev: 1249 > 1245)
```

## Environment Variables
All command line options can be set via environment variables:
```bash
ETCD_FDW_POSTGRES_DSN="postgres://user:pass@localhost/db"
ETCD_FDW_ETCD_DSN="etcd://localhost:2379/"
ETCD_FDW_LOG_LEVEL="info"
ETCD_FDW_DRY_RUN="false"
```

## Connection String Formats

### PostgreSQL DSN
Standard PostgreSQL connection string format:
```
postgres://[user[:password]@][host][:port][/database][?param1=value1&param2=value2]
```

### etcd DSN  
Custom format for etcd connection:
```
etcd://host1:port1[,host2:port2][/prefix][?param1=value1&param2=value2]
```

**Supported etcd parameters:**
- `tls=enabled|disabled` - Enable TLS connection
- `dial_timeout=5s` - Connection timeout
- `request_timeout=10s` - Request timeout
- `username=user` - Authentication username
- `password=pass` - Authentication password

## Database Schema
The program expects these PostgreSQL tables to exist:

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
    status text NOT NULL DEFAULT 'pending'
);

-- Indexes
CREATE INDEX idx_etcd_key_revision ON etcd(key, revision DESC);
CREATE INDEX idx_etcd_wal_status ON etcd_wal(status) WHERE status = 'pending';

-- Trigger for notifications
CREATE OR REPLACE FUNCTION notify_etcd_change()
RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('etcd_changes', 
        json_build_object(
            'id', NEW.id,
            'key', NEW.key,
            'value', NEW.value,
            'revision', NEW.revision
        )::text
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER etcd_wal_notify
    AFTER INSERT ON etcd_wal
    FOR EACH ROW
    EXECUTE FUNCTION notify_etcd_change();
```
