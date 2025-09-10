-- Main etcd table for key-value storage with revision history
CREATE TABLE etcd (
	ts timestamp with time zone NOT NULL DEFAULT now(),
	key text NOT NULL,
	value text,
	revision bigint NOT NULL,
	tombstone boolean NOT NULL DEFAULT false,
	PRIMARY KEY(key, revision)
);

-- Write-ahead log table for tracking changes to be synchronized
CREATE TABLE etcd_wal (
	ts timestamp with time zone NOT NULL DEFAULT now(),
	key text NOT NULL,
	value text,
	revision bigint, -- Current revision before modification (null = new key)
	PRIMARY KEY(key, ts)
);

-- Performance indexes
CREATE INDEX idx_etcd_key_revision ON etcd(key, revision DESC);
CREATE INDEX idx_etcd_wal_key ON etcd_wal(key);
CREATE INDEX idx_etcd_wal_ts ON etcd_wal(ts);

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
