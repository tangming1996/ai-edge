CREATE TABLE tasks (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    parent_task_id      UUID REFERENCES tasks(id),
    scope               TEXT NOT NULL DEFAULT 'Node',
    type                TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'Pending',
    target_gateway_id   UUID REFERENCES gateways(id),
    target_node_id      UUID REFERENCES edge_nodes(id),
    payload             JSONB NOT NULL DEFAULT '{}',
    result              JSONB,

    -- Claim fields (for NodeTask dispatch by gateway-runtime)
    dispatch_status     TEXT NOT NULL DEFAULT 'Unclaimed',
    owner_instance      TEXT,
    claim_expire_at     TIMESTAMPTZ,

    -- Retry & timeout
    max_retries         INT NOT NULL DEFAULT 3,
    retry_count         INT NOT NULL DEFAULT 0,
    timeout_seconds     INT NOT NULL DEFAULT 600,

    -- Idempotency
    idempotency_key     TEXT,

    created_by          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Key query indexes (task 2.4)
CREATE INDEX idx_tasks_gateway_status_created
    ON tasks (target_gateway_id, status, created_at);

CREATE INDEX idx_tasks_node_status_created
    ON tasks (target_node_id, status, created_at);

-- Partial index for NodeTask claim dispatch
CREATE INDEX idx_tasks_gateway_dispatch_node
    ON tasks (target_gateway_id, dispatch_status)
    WHERE scope = 'Node';
