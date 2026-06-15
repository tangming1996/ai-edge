CREATE TABLE gateways (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    region      TEXT NOT NULL,
    labels      JSONB NOT NULL DEFAULT '{}',
    status      TEXT NOT NULL DEFAULT 'Active',
    endpoint    TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_gateways_region ON gateways (region);
