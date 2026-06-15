CREATE TABLE edge_runtime_states (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id         UUID NOT NULL REFERENCES edge_nodes(id),
    model_name      TEXT NOT NULL,
    model_version   TEXT NOT NULL,
    runtime         TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'Unknown',
    metrics         JSONB NOT NULL DEFAULT '{}',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (node_id, model_name, model_version)
);

CREATE INDEX idx_edge_runtime_states_node ON edge_runtime_states (node_id);
CREATE INDEX idx_edge_runtime_states_model ON edge_runtime_states (model_name, model_version);
