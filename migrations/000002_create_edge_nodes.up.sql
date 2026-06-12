CREATE TABLE edge_nodes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gateway_id      UUID NOT NULL REFERENCES gateways(id),
    name            TEXT NOT NULL DEFAULT '',
    labels          JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'Pending',
    online          BOOLEAN NOT NULL DEFAULT false,
    agent_version   TEXT NOT NULL DEFAULT '',
    hardware_info   JSONB NOT NULL DEFAULT '{}',
    last_seen_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_edge_nodes_gateway_id ON edge_nodes (gateway_id);
CREATE INDEX idx_edge_nodes_status ON edge_nodes (status);
