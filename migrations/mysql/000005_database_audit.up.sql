ALTER TABLE audit_records ADD COLUMN deleted_at DATETIME(6) NULL, ADD COLUMN deleted_by TEXT NULL;
ALTER TABLE audit_record_keys ADD COLUMN deleted_at DATETIME(6) NULL, ADD COLUMN deleted_by TEXT NULL;
CREATE TRIGGER audit_records_audit_bi BEFORE INSERT ON audit_records FOR EACH ROW SET NEW.created_at=CURRENT_TIMESTAMP(6),NEW.updated_at=CURRENT_TIMESTAMP(6),NEW.created_by=NULLIF(TRIM(@app_actor_id),''),NEW.updated_by=NULLIF(TRIM(@app_actor_id),''),NEW.version=1,NEW.deleted_at=NULL,NEW.deleted_by=NULL;
CREATE TRIGGER audit_records_audit_bu BEFORE UPDATE ON audit_records FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='audit rows are immutable';
CREATE TRIGGER audit_records_audit_bd BEFORE DELETE ON audit_records FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='physical deletion is restricted to an approved retention migration';
CREATE TRIGGER audit_record_keys_audit_bi BEFORE INSERT ON audit_record_keys FOR EACH ROW SET NEW.created_at=CURRENT_TIMESTAMP(6),NEW.updated_at=CURRENT_TIMESTAMP(6),NEW.created_by=NULLIF(TRIM(@app_actor_id),''),NEW.updated_by=NULLIF(TRIM(@app_actor_id),''),NEW.version=1,NEW.deleted_at=NULL,NEW.deleted_by=NULL;
CREATE TRIGGER audit_record_keys_audit_bu BEFORE UPDATE ON audit_record_keys FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='audit rows are immutable';
CREATE TRIGGER audit_record_keys_audit_bd BEFORE DELETE ON audit_record_keys FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='physical deletion is restricted to an approved retention migration';
