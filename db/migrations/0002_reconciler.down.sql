BEGIN;

DROP TRIGGER IF EXISTS trg_notify_new_outbox ON outbox;
DROP FUNCTION IF EXISTS notify_new_outbox();

COMMIT;
