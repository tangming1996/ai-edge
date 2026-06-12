CREATE TABLE bootstrap_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gateway_id      UUID NOT NULL REFERENCES gateways(id),
    token_hash      TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    labels          JSONB NOT NULL DEFAULT '{}',
    max_uses        INT NOT NULL DEFAULT 1,
    used_count      INT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'Active',
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_bootstrap_tokens_token_hash UNIQUE (token_hash)
);

CREATE INDEX idx_bootstrap_tokens_gateway_id ON bootstrap_tokens (gateway_id);
