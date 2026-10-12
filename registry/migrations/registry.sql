CREATE TABLE IF NOT EXISTS resources(
 kind text NOT NULL, id text NOT NULL, revision integer NOT NULL, spec jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(kind,id,revision));
CREATE TABLE IF NOT EXISTS secrets(name text PRIMARY KEY, sealed bytea NOT NULL, updated_at timestamptz NOT NULL DEFAULT now());
CREATE UNIQUE INDEX IF NOT EXISTS knowledge_graph_endpoint ON resources((spec->>'endpoint')) WHERE kind='knowledge' AND spec->>'engine'='lightrag';
-- Keyset index for bounded catalog pages; full resource bodies stay off the wire.
CREATE INDEX IF NOT EXISTS resources_catalog_key ON resources (((kind||':'||id) COLLATE "C"), revision DESC);
