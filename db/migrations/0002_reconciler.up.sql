-- 0002_reconciler.up.sql
-- The reconciler is woken by NOTIFY when new outbox rows commit — the
-- transactional-outbox push side (docs/ARCHITECTURE.md §4). Polling remains
-- as a safety net; NOTIFY just removes the latency.

BEGIN;

CREATE OR REPLACE FUNCTION notify_new_outbox() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('new_outbox', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_notify_new_outbox
AFTER INSERT ON outbox
FOR EACH ROW EXECUTE FUNCTION notify_new_outbox();

COMMIT;
