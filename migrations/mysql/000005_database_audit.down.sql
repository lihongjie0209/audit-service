DROP TRIGGER IF EXISTS audit_record_keys_audit_bd;
DROP TRIGGER IF EXISTS audit_record_keys_audit_bu;
DROP TRIGGER IF EXISTS audit_record_keys_audit_bi;
DROP TRIGGER IF EXISTS audit_records_audit_bd;
DROP TRIGGER IF EXISTS audit_records_audit_bu;
DROP TRIGGER IF EXISTS audit_records_audit_bi;
ALTER TABLE audit_record_keys DROP COLUMN deleted_by, DROP COLUMN deleted_at;
ALTER TABLE audit_records DROP COLUMN deleted_by, DROP COLUMN deleted_at;
