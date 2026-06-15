CREATE TABLE model_deployments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_name      TEXT NOT NULL,
    model_version   TEXT NOT NULL,
    target          JSONB NOT NULL DEFAULT '{}',
    runtime         TEXT NOT NULL DEFAULT 'auto',
    rollout         JSONB NOT NULL DEFAULT '{"max_unavailable": 1}',
    status          JSONB NOT NULL DEFAULT '{"desired_nodes": 0, "ready_nodes": 0, "failed_nodes": 0, "phase": "Pending"}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_model_deployments_model ON model_deployments (model_name, model_version);
CREATE INDEX idx_model_deployments_phase ON model_deployments ((status->>'phase'));
