ALTER TABLE audit_records ADD COLUMN application_id VARCHAR(64) NOT NULL DEFAULT '' AFTER tenant_id, ADD INDEX audit_records_application_time_idx (tenant_id, application_id, occurred_at DESC, id);
