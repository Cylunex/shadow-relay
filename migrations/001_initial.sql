CREATE TABLE IF NOT EXISTS catalogs (id text PRIMARY KEY, data jsonb NOT NULL);
CREATE TABLE IF NOT EXISTS runtimes (id text PRIMARY KEY, data jsonb NOT NULL);
CREATE TABLE IF NOT EXISTS sources (id text PRIMARY KEY, data jsonb NOT NULL,
 runtime_id text GENERATED ALWAYS AS (NULLIF(data->>'runtimeId','')) STORED REFERENCES runtimes(id),
 catalog_id text GENERATED ALWAYS AS (NULLIF(data->>'catalogId','')) STORED REFERENCES catalogs(id));
CREATE TABLE IF NOT EXISTS endpoints (id text PRIMARY KEY, data jsonb NOT NULL,
 source_id text GENERATED ALWAYS AS (data->>'sourceId') STORED NOT NULL REFERENCES sources(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS secrets (id text PRIMARY KEY, data jsonb NOT NULL);
CREATE TABLE IF NOT EXISTS revisions (id text PRIMARY KEY, data jsonb NOT NULL,
 source_id text GENERATED ALWAYS AS (data->>'sourceId') STORED NOT NULL REFERENCES sources(id) ON DELETE CASCADE);
CREATE INDEX IF NOT EXISTS revisions_source ON revisions(source_id);
CREATE TABLE IF NOT EXISTS candidates (id text PRIMARY KEY, data jsonb NOT NULL,
 catalog_id text GENERATED ALWAYS AS (data->>'catalogId') STORED NOT NULL REFERENCES catalogs(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS probes (id text PRIMARY KEY, data jsonb NOT NULL,
 source_id text GENERATED ALWAYS AS (data->>'sourceId') STORED NOT NULL REFERENCES sources(id) ON DELETE CASCADE);
CREATE INDEX IF NOT EXISTS probes_source ON probes(source_id);
CREATE TABLE IF NOT EXISTS source_sets (id text PRIMARY KEY, data jsonb NOT NULL);
CREATE TABLE IF NOT EXISTS publications (id text PRIMARY KEY, data jsonb NOT NULL,
 set_id text GENERATED ALWAYS AS (data->>'setId') STORED NOT NULL REFERENCES source_sets(id));
CREATE TABLE IF NOT EXISTS bindings (id text PRIMARY KEY, data jsonb NOT NULL,
 set_id text GENERATED ALWAYS AS (data->>'setId') STORED NOT NULL REFERENCES source_sets(id),
 token_hash text GENERATED ALWAYS AS (data->>'tokenHash') STORED UNIQUE NOT NULL);
CREATE TABLE IF NOT EXISTS audits (id text PRIMARY KEY, data jsonb NOT NULL);
CREATE TABLE IF NOT EXISTS jobs (
 id text PRIMARY KEY, kind text NOT NULL, target_id text NOT NULL,
 status text NOT NULL DEFAULT 'queued', attempts integer NOT NULL DEFAULT 0,
 available_at timestamptz NOT NULL DEFAULT now(), lease_until timestamptz,
 lease_token text, error text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), finished_at timestamptz);
CREATE UNIQUE INDEX IF NOT EXISTS jobs_active_target ON jobs(kind,target_id) WHERE status IN ('queued','running');
CREATE INDEX IF NOT EXISTS jobs_ready ON jobs(available_at) WHERE status='queued';
