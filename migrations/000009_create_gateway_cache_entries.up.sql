CREATE TABLE gateway_cache_entries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gateway_id      UUID NOT NULL REFERENCES gateways(id),
    model_id        UUID NOT NULL REFERENCES models(id),
    version         TEXT NOT NULL,
    cached_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_access_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    size_bytes      BIGINT NOT NULL DEFAULT 0,

    UNIQUE (gateway_id, model_id, version)
);

CREATE INDEX idx_cache_entries_gateway_lru ON gateway_cache_entries (gateway_id, last_access_at);
