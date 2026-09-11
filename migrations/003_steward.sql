-- Personal steward: job generation for coalesce, preference overlays, soft state.
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS generation bigint NOT NULL DEFAULT 0;
CREATE TABLE IF NOT EXISTS preferences (
  id text PRIMARY KEY,
  data jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS deleted_sources (
  id text PRIMARY KEY,
  data jsonb NOT NULL,
  deleted_at timestamptz NOT NULL DEFAULT now()
);
