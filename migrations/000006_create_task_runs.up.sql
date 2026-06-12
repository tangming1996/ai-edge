CREATE TABLE task_runs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID NOT NULL REFERENCES tasks(id),
    node_id     UUID REFERENCES edge_nodes(id),
    attempt     INT NOT NULL DEFAULT 1,
    status      TEXT NOT NULL DEFAULT 'Running',
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    error_msg   TEXT,
    result      JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_task_runs_task_id ON task_runs (task_id);
CREATE INDEX idx_task_runs_node_id ON task_runs (node_id);
