CREATE TABLE gateway_runtime_instances (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    gateway_id          UUID NOT NULL REFERENCES gateways(id),
    instance_id         TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'Unknown',
    last_heartbeat_at   TIMESTAMPTZ,

    UNIQUE (gateway_id, instance_id)
);

CREATE INDEX idx_gateway_runtime_instances_gateway ON gateway_runtime_instances (gateway_id);
CREATE INDEX idx_gateway_runtime_instances_status ON gateway_runtime_instances (status);
