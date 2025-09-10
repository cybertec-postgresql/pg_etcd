# Quickstart Guide: Bidirectional Synchronization Between etcd and PostgreSQL

## Prerequisites

- Go 1.21+ installed
- PostgreSQL 12+ running
- etcd 3.5+ cluster running
- Network connectivity between all components

## Installation

### 1. Build from Source
```bash
git clone https://github.com/your-org/etcd-fdw.git
cd etcd-fdw
go build -o etcd-sync ./cmd/etcd-sync
```

### 2. Install Dependencies
```bash
# Install PostgreSQL (Ubuntu/Debian)
sudo apt-get install postgresql postgresql-contrib

# Install etcd (using releases)
wget https://github.com/etcd-io/etcd/releases/download/v3.5.10/etcd-v3.5.10-linux-amd64.tar.gz
tar -xzf etcd-v3.5.10-linux-amd64.tar.gz
sudo mv etcd-v3.5.10-linux-amd64/etcd* /usr/local/bin/
```

## Database Setup

### 1. Create Database and User
```sql
-- Connect to PostgreSQL as superuser
sudo -u postgres psql

-- Create database and user
CREATE DATABASE sync_db;
CREATE USER sync_user WITH ENCRYPTED PASSWORD 'your_password';
GRANT ALL PRIVILEGES ON DATABASE sync_db TO sync_user;

-- Connect to the sync database
\c sync_db

-- Create the required tables
CREATE TABLE etcd (
    ts timestamp with time zone NOT NULL DEFAULT now(),
    key text NOT NULL,
    value text,
    revision bigint NOT NULL,
    tombstone boolean NOT NULL DEFAULT false,
    PRIMARY KEY(key, revision)
);

CREATE TABLE etcd_wal (
    id serial PRIMARY KEY,
    ts timestamp with time zone NOT NULL DEFAULT now(),
    key text NOT NULL,
    value text,
    revision bigint,
    status text NOT NULL DEFAULT 'pending',
    retry_count integer NOT NULL DEFAULT 0,
    error_message text
);

-- Create indexes for performance
CREATE INDEX idx_etcd_key_revision ON etcd(key, revision DESC);
CREATE INDEX idx_etcd_wal_status ON etcd_wal(status) WHERE status = 'pending';

-- Create trigger function for notifications
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

-- Create trigger
CREATE TRIGGER etcd_wal_notify
    AFTER INSERT OR UPDATE ON etcd_wal
    FOR EACH ROW
    EXECUTE FUNCTION notify_etcd_change();
```

### 2. Grant Permissions
```sql
-- Grant permissions to sync_user
GRANT ALL ON TABLE etcd TO sync_user;
GRANT ALL ON TABLE etcd_wal TO sync_user;
GRANT USAGE, SELECT ON SEQUENCE etcd_wal_id_seq TO sync_user;
```

## etcd Setup

### 1. Start etcd (Development)
```bash
# Start single-node etcd for testing
etcd --name etcd1 \
  --data-dir /tmp/etcd1.etcd \
  --listen-client-urls http://localhost:2379 \
  --advertise-client-urls http://localhost:2379 \
  --listen-peer-urls http://localhost:2380 \
  --initial-advertise-peer-urls http://localhost:2380 \
  --initial-cluster etcd1=http://localhost:2380 \
  --initial-cluster-token etcd-cluster-1 \
  --initial-cluster-state new
```

### 2. Verify etcd is Running
```bash
# Test etcd connectivity
etcdctl --endpoints=localhost:2379 endpoint health
etcdctl --endpoints=localhost:2379 put /test/key "test-value"
etcdctl --endpoints=localhost:2379 get /test/key
```

## Configuration

### 1. Create Configuration File
```bash
mkdir -p /etc/etcd-sync
cat > /etc/etcd-sync/config.yaml << EOF
postgresql:
  host: localhost
  port: 5432
  database: sync_db
  username: sync_user
  password: your_password
  ssl_mode: disable
  max_connections: 10

etcd:
  endpoints:
    - localhost:2379
  dial_timeout: 5s
  request_timeout: 10s

sync:
  direction: bidirectional
  conflict_resolution: etcd_wins
  retry:
    max_attempts: 3
    initial_backoff: 1s
    max_backoff: 30s
  batch_size: 100
  sync_interval: 5s

logging:
  level: info
  format: text
  output: stdout
EOF
```

### 2. Validate Configuration
```bash
./etcd-sync config validate
```

## Running the Service

### 1. Start Synchronization Service
```bash
# Start in foreground for testing
./etcd-sync start --config /etc/etcd-sync/config.yaml

# Or start as daemon
./etcd-sync start --daemon --pid-file /var/run/etcd-sync.pid
```

### 2. Check Service Status
```bash
./etcd-sync status --format json
```

## Testing Synchronization

### Test 1: etcd to PostgreSQL Sync

```bash
# Add data to etcd
etcdctl put /config/app/timeout "30s"
etcdctl put /config/app/retries "3"
etcdctl put /config/db/host "localhost"

# Verify data appears in PostgreSQL
psql -U sync_user -d sync_db -c "SELECT * FROM etcd ORDER BY key, revision;"
```

Expected output:
```
           ts            |        key        | value  | revision | tombstone 
-------------------------+-------------------+--------+----------+-----------
 2025-09-10 15:30:01.123 | /config/app/timeout| 30s   |    1001  | f
 2025-09-10 15:30:01.456 | /config/app/retries| 3     |    1002  | f
 2025-09-10 15:30:01.789 | /config/db/host   | localhost | 1003 | f
```

### Test 2: PostgreSQL to etcd Sync

```bash
# Add data via PostgreSQL (simulating application write)
psql -U sync_user -d sync_db -c "
INSERT INTO etcd_wal (key, value, revision) 
VALUES ('/config/app/debug', 'true', NULL);
"

# Check WAL entry was created
psql -U sync_user -d sync_db -c "SELECT * FROM etcd_wal WHERE status = 'pending';"

# Verify data appears in etcd (after sync service processes it)
etcdctl get /config/app/debug

# Check WAL entry status updated
psql -U sync_user -d sync_db -c "SELECT * FROM etcd_wal ORDER BY ts DESC LIMIT 1;"
```

### Test 3: Conflict Resolution

```bash
# Create initial key in both systems
etcdctl put /config/test/conflict "etcd-value"
psql -U sync_user -d sync_db -c "
INSERT INTO etcd_wal (key, value, revision) 
VALUES ('/config/test/conflict', 'pg-value', 1005);
"

# Check conflict was detected and resolved
./etcd-sync status --format json | jq '.synchronization.conflicts_resolved'

# Verify winning value (should be etcd value based on config)
etcdctl get /config/test/conflict
```

### Test 4: Service Recovery

```bash
# Stop the service
pkill etcd-sync

# Make changes while service is down
etcdctl put /config/test/offline "offline-change"

# Restart service
./etcd-sync start --config /etc/etcd-sync/config.yaml &

# Verify offline changes are synchronized
psql -U sync_user -d sync_db -c "SELECT * FROM etcd WHERE key = '/config/test/offline';"
```

## Monitoring and Troubleshooting

### 1. Check Sync Status
```bash
# View sync statistics
./etcd-sync status

# Continuous monitoring
./etcd-sync status --watch --interval 2
```

### 2. Validate Data Consistency
```bash
# Run consistency check
./etcd-sync validate

# Fix any inconsistencies found
./etcd-sync validate --fix
```

### 3. View Logs
```bash
# Follow service logs
tail -f /var/log/etcd-sync/sync.log

# View audit log
tail -f /var/log/etcd-sync/audit.log
```

### 4. Common Issues

**Issue**: "Connection refused to PostgreSQL"
```bash
# Check PostgreSQL is running
sudo systemctl status postgresql

# Check connection details
psql -U sync_user -d sync_db -h localhost -p 5432
```

**Issue**: "etcd cluster unreachable"
```bash
# Check etcd health
etcdctl --endpoints=localhost:2379 endpoint health

# Check network connectivity
telnet localhost 2379
```

**Issue**: "Pending operations not processing"
```bash
# Check WAL table for errors
psql -U sync_user -d sync_db -c "
SELECT key, status, retry_count, error_message 
FROM etcd_wal 
WHERE status != 'success' 
ORDER BY ts DESC;
"

# Manually retry failed operations
./etcd-sync sync --force
```

## Production Deployment

### 1. Service Configuration
```bash
# Create systemd service file
sudo cat > /etc/systemd/system/etcd-sync.service << EOF
[Unit]
Description=etcd PostgreSQL Synchronization Service
After=network.target postgresql.service etcd.service

[Service]
Type=simple
User=etcd-sync
Group=etcd-sync
ExecStart=/usr/local/bin/etcd-sync start --config /etc/etcd-sync/config.yaml
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

# Enable and start service
sudo systemctl enable etcd-sync
sudo systemctl start etcd-sync
```

### 2. Security Hardening
```bash
# Create dedicated user
sudo useradd --system --shell /bin/false etcd-sync

# Set file permissions
sudo chown -R etcd-sync:etcd-sync /etc/etcd-sync
sudo chmod 600 /etc/etcd-sync/config.yaml

# Configure TLS for etcd (production)
# Update config.yaml with TLS certificates
```

### 3. Backup and Recovery
```bash
# Backup PostgreSQL sync data
pg_dump -U sync_user sync_db > sync_backup.sql

# Backup etcd cluster
etcdctl snapshot save etcd_backup.db

# Test restore procedures regularly
```

This completes the basic setup and testing of the bidirectional synchronization system. The service will now maintain consistency between etcd and PostgreSQL automatically.
