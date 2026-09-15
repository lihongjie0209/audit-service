DROP TRIGGER IF EXISTS audit_record_keys_immutable_row ON audit_record_keys;
DROP TRIGGER IF EXISTS audit_records_immutable_row ON audit_records;
DROP FUNCTION IF EXISTS audit_immutable_row();
ALTER TABLE audit_record_keys DROP COLUMN deleted_by, DROP COLUMN deleted_at;
ALTER TABLE audit_records DROP COLUMN deleted_by, DROP COLUMN deleted_at;
