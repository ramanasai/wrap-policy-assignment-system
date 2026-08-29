-- 0004_segments.up.sql
-- Groups/Supergroups: named, reusable rule predicates whose membership is
-- DERIVED state (rebuilt by the worker from the predicate, never hand-edited).
-- This closes the "group membership changes" reconciliation requirement:
-- membership changes are events just like fact changes.
--
-- Rules match membership via the derived `segments` attribute + `contains`
-- (e.g. {"attr":"segments","op":"contains","value":"field_ops"}), so the
-- resolver stays pure — segments arrive as ordinary list facts.

BEGIN;

CREATE TABLE segment (
    id          TEXT PRIMARY KEY,
    company_id  TEXT NOT NULL,
    name        TEXT NOT NULL,
    predicate   JSONB NOT NULL,          -- canonical rule AST (same as rule predicates)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE segment_membership (
    segment_id  TEXT NOT NULL,
    employee_id TEXT NOT NULL,
    PRIMARY KEY (segment_id, employee_id)
);

CREATE INDEX idx_segment_membership_employee ON segment_membership (employee_id);

-- Derived attribute injected by the repo layer at FactsAt time.
INSERT INTO attribute_definition (key, value_type, allowed_ops, description) VALUES
    ('segments', 'string', '{contains}', 'Derived list of segment memberships (Supergroups)');

COMMIT;