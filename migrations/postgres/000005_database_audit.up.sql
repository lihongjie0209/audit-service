ALTER TABLE audit_records ADD COLUMN deleted_at TIMESTAMPTZ, ADD COLUMN deleted_by TEXT;
ALTER TABLE audit_record_keys ADD COLUMN deleted_at TIMESTAMPTZ, ADD COLUMN deleted_by TEXT;

CREATE OR REPLACE FUNCTION audit_immutable_row()
RETURNS trigger LANGUAGE plpgsql AS $audit$
DECLARE actor_id TEXT := NULLIF(current_setting('app.actor_id', true), '');
BEGIN
  IF TG_OP = 'DELETE' THEN RAISE EXCEPTION 'physical deletion of audit rows is forbidden; use partition retention'; END IF;
  IF TG_OP = 'UPDATE' THEN RAISE EXCEPTION 'audit rows are immutable'; END IF;
  IF actor_id IS NULL THEN RAISE EXCEPTION 'app.actor_id must be set for audited writes'; END IF;
  NEW.created_at := statement_timestamp();
  NEW.updated_at := NEW.created_at;
  NEW.created_by := actor_id;
  NEW.updated_by := actor_id;
  NEW.version := 1;
  NEW.deleted_at := NULL;
  NEW.deleted_by := NULL;
  RETURN NEW;
END;
$audit$;

CREATE TRIGGER audit_records_immutable_row BEFORE INSERT OR UPDATE OR DELETE ON audit_records FOR EACH ROW EXECUTE FUNCTION audit_immutable_row();
CREATE TRIGGER audit_record_keys_immutable_row BEFORE INSERT OR UPDATE OR DELETE ON audit_record_keys FOR EACH ROW EXECUTE FUNCTION audit_immutable_row();
