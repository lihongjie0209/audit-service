CREATE TABLE audit_records (
    id UUID NOT NULL,
    tenant_id TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    actor_type TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    request_id TEXT NOT NULL DEFAULT '',
    trace_id TEXT NOT NULL DEFAULT '',
    source_service TEXT NOT NULL,
    before_summary JSONB,
    after_summary JSONB,
    occurred_at TIMESTAMPTZ NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    created_by TEXT NOT NULL,
    updated_by TEXT NOT NULL,
    PRIMARY KEY (id, occurred_at)
) PARTITION BY RANGE (occurred_at);

CREATE TABLE audit_records_default PARTITION OF audit_records DEFAULT;
CREATE INDEX audit_records_tenant_time_idx ON audit_records (tenant_id, occurred_at DESC, id);
CREATE INDEX audit_records_resource_idx ON audit_records (tenant_id, resource_type, resource_id, occurred_at DESC);
CREATE INDEX audit_records_actor_idx ON audit_records (tenant_id, actor_id, occurred_at DESC);
CREATE INDEX audit_records_request_idx ON audit_records (request_id) WHERE request_id <> '';

COMMENT ON TABLE audit_records IS 'Monthly partitions are created ahead of time; the default partition prevents write loss. Retention/archive is deployment policy and may be automated with pg_partman.';
