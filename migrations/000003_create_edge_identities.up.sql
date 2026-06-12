CREATE TABLE edge_identities (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id         UUID NOT NULL REFERENCES edge_nodes(id),
    gateway_id      UUID NOT NULL REFERENCES gateways(id),
    serial          TEXT NOT NULL DEFAULT '',
    fingerprint     TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'Active',
    certificate_pem TEXT NOT NULL DEFAULT '',
    issued_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at      TIMESTAMPTZ NOT NULL,
    revoked_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_edge_identities_node_id UNIQUE (node_id),
    CONSTRAINT uq_edge_identities_fingerprint UNIQUE (fingerprint)
);

-- Partial unique: only one Active/Suspended identity per serial
CREATE UNIQUE INDEX uq_edge_identities_serial_active
    ON edge_identities (serial)
    WHERE status IN ('Active', 'Suspended');

CREATE INDEX idx_edge_identities_gateway_id ON edge_identities (gateway_id);
