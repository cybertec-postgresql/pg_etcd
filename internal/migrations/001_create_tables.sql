-- Main etcd table for key-value storage with revision history
CREATE TABLE etcd (
	ts timestamp with time zone NOT NULL DEFAULT now(),
	key text NOT NULL,
	value text NOT NULL,
	revision bigint NOT NULL,
	tombstone boolean NOT NULL DEFAULT false,
	PRIMARY KEY(key, revision)
);


-- Performance indexes
CREATE INDEX idx_etcd_ts ON etcd(ts);
CREATE INDEX idx_etcd_pending ON etcd(key) WHERE revision = -1;

-- Function: Get latest value for a key
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

-- Function: Insert record with pending status (revision = -1)
-- Applications use this to insert data that needs sync to etcd
CREATE OR REPLACE FUNCTION etcd_put(p_key text, p_value text)
RETURNS timestamp with time zone
LANGUAGE sql AS $$
	INSERT INTO etcd (key, value, revision, tombstone)
	VALUES (p_key, p_value, -1, false)
	RETURNING ts;
$$;

-- Function: Mark key for deletion with pending status
CREATE OR REPLACE FUNCTION etcd_delete(p_key text)
RETURNS timestamp with time zone
LANGUAGE sql AS $$
	INSERT INTO etcd (key, value, revision, tombstone)
	VALUES (p_key, NULL, -1, true)
	RETURNING ts;
$$;

-- Function: Get pending records for sync to etcd
CREATE OR REPLACE FUNCTION etcd_get_pending()
RETURNS TABLE(key text, value text, ts timestamp with time zone, tombstone boolean)
LANGUAGE sql STABLE AS $$
	SELECT e.key, e.value, e.ts, e.tombstone
	FROM etcd e
	WHERE e.revision = -1
	ORDER BY e.ts ASC;
$$;

-- Function: Update revision after successful sync to etcd
CREATE OR REPLACE FUNCTION etcd_update_revision(p_key text, p_timestamp timestamp with time zone, p_revision bigint)
RETURNS boolean
LANGUAGE plpgsql AS $$
DECLARE
    row_count integer;
BEGIN
    UPDATE etcd 
    SET revision = p_revision 
    WHERE key = p_key AND ts = p_timestamp AND revision = -1;
    
    GET DIAGNOSTICS row_count = ROW_COUNT;
    RETURN row_count > 0;
END;
$$;

