CREATE TABLE models (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    version         TEXT NOT NULL,
    format          TEXT NOT NULL DEFAULT 'CUSTOM',
    checksum        TEXT NOT NULL DEFAULT '',
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    artifact_uri    TEXT NOT NULL,
    signature_uri   TEXT NOT NULL DEFAULT '',
    labels          JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (name, version)
);

CREATE INDEX idx_models_name ON models (name);
CREATE INDEX idx_models_format ON models (format);
