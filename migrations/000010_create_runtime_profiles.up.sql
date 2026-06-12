CREATE TABLE runtime_profiles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    selector        JSONB NOT NULL DEFAULT '{}',
    runtime         TEXT NOT NULL,
    priority        INT NOT NULL DEFAULT 0,
    runtime_config  JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_runtime_profiles_runtime ON runtime_profiles (runtime);
CREATE INDEX idx_runtime_profiles_priority ON runtime_profiles (priority DESC);
