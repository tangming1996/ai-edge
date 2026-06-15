CREATE TABLE task_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     UUID NOT NULL REFERENCES tasks(id),
    event_type  TEXT NOT NULL,
    old_status  TEXT,
    new_status  TEXT,
    actor       TEXT NOT NULL DEFAULT 'system',
    detail      JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_task_events_task_created
    ON task_events (task_id, created_at);
