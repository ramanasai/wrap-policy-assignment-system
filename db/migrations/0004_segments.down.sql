BEGIN;

DELETE FROM attribute_definition WHERE key = 'segments';
DROP TABLE IF EXISTS segment_membership;
DROP TABLE IF EXISTS segment;

COMMIT;