ALTER TABLE audit_records ADD COLUMN application_id TEXT NOT NULL DEFAULT '';
CREATE INDEX audit_records_application_time_idx ON audit_records (tenant_id, application_id, occurred_at DESC, id);
