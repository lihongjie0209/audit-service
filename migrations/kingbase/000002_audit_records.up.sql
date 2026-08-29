CREATE TABLE audit_records (
    id UUID PRIMARY KEY,
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
    updated_by TEXT NOT NULL
);
CREATE INDEX audit_records_tenant_time_idx ON audit_records (tenant_id, occurred_at DESC, id);
CREATE INDEX audit_records_resource_idx ON audit_records (tenant_id, resource_type, resource_id, occurred_at DESC);
CREATE INDEX audit_records_actor_idx ON audit_records (tenant_id, actor_id, occurred_at DESC);
