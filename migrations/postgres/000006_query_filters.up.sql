CREATE INDEX audit_records_source_time_idx ON audit_records (tenant_id, application_id, source_service, occurred_at DESC);
CREATE INDEX audit_records_trace_idx ON audit_records (tenant_id, application_id, trace_id, occurred_at DESC) WHERE trace_id <> '';
