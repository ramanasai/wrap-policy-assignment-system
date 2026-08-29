-- Policy Assignment System — initial schema (0001_init.up.sql)
-- Principles: bitemporal + append-only; semantics are data (policy_category);
-- assignments are derived projections; traces are written at decision time.

BEGIN;

-- ============================================================
-- Policy semantics (declared, immutable-after-use)
-- ============================================================

CREATE TABLE policy_category (
    id                  TEXT PRIMARY KEY,          -- 'pay_schedule', 'app_access', ...
    display_name        TEXT NOT NULL,
    cardinality         TEXT NOT NULL CHECK (cardinality IN ('single', 'many')),
    resolution_strategy TEXT NOT NULL CHECK (resolution_strategy IN
                            ('priority_rank', 'explicit_user_choice', 'additive')),
    default_priority    INT  NOT NULL DEFAULT 0,
    tiebreaker          TEXT NOT NULL DEFAULT 'priority_then_id',
    immutable_after_use BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE attribute_definition (
    key         TEXT PRIMARY KEY,                 -- 'location', 'department', ...
    value_type  TEXT NOT NULL CHECK (value_type IN ('string', 'number', 'date', 'bool', 'enum')),
    allowed_ops TEXT[] NOT NULL DEFAULT '{eq,ne,in,not_in}',  -- ops the rule builder may expose
    enum_values JSONB,                            -- for value_type = 'enum'
    description TEXT
);

-- ============================================================
-- Employees & bitemporal facts
-- ============================================================

CREATE TABLE employee (
    id         TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    hired_on   DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_employee_company ON employee (company_id);

-- Bitemporal: valid_range = business truth, recorded_at = second clock.
-- Corrections append a new event and set superseded_by on the old one.
CREATE TABLE fact_event (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    employee_id   TEXT NOT NULL,
    attribute_key TEXT NOT NULL,
    value         JSONB NOT NULL,
    valid_range   DATERANGE NOT NULL,
    recorded_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    superseded_by BIGINT,
    trigger       TEXT NOT NULL DEFAULT 'hr_edit' CHECK (trigger IN
                      ('hr_edit', 'tenure_gate', 'system', 'correction'))
);

-- "Facts as of date D" is an index scan.
CREATE INDEX idx_fact_event_asof ON fact_event (employee_id, attribute_key, valid_range);

-- ============================================================
-- Policies & rules (versioned, effective-dated)
-- ============================================================

CREATE TABLE policy (
    id          TEXT PRIMARY KEY,
    category_id TEXT NOT NULL,
    name        TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}'       -- category-specific config
);

CREATE INDEX idx_policy_category ON policy (category_id);

CREATE TABLE policy_version (
    id          TEXT PRIMARY KEY,
    policy_id   TEXT NOT NULL,
    version     INT  NOT NULL,
    payload     JSONB NOT NULL,
    valid_range DATERANGE NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (policy_id, version)
);

CREATE TABLE assignment_rule (
    id            TEXT PRIMARY KEY,
    company_id    TEXT NOT NULL,
    category_id   TEXT NOT NULL,
    policy_id     TEXT NOT NULL,
    source        TEXT NOT NULL DEFAULT 'authored' CHECK (source IN ('authored', 'manual', 'system')),
    priority      INT  NOT NULL DEFAULT 0,        -- explicit; ties broken below
    specificity   INT,                           -- computed from AST, cached per version
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Deterministic tiebreak: (priority DESC, specificity DESC, created_at, id) — all stored.
CREATE INDEX idx_rule_tiebreak ON assignment_rule (category_id, priority DESC, created_at, id);

CREATE TABLE rule_version (
    id            TEXT PRIMARY KEY,
    rule_id       TEXT NOT NULL,
    version       INT  NOT NULL,
    predicate     JSONB NOT NULL,                 -- canonical rule AST
    valid_range   DATERANGE NOT NULL,             -- 'takes effect Jan 1' = future interval
    recorded_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (rule_id, version)
);

CREATE INDEX idx_rule_version_valid ON rule_version (rule_id, valid_range);

-- ============================================================
-- Derived assignments (PROJECTION — rebuildable, never authoritative)
-- ============================================================

CREATE TABLE assignment (
    employee_id       TEXT NOT NULL,
    category_id       TEXT NOT NULL,
    policy_id         TEXT NOT NULL,
    policy_version_id TEXT NOT NULL,
    rule_id           TEXT NOT NULL,             -- even for manual overrides
    valid_range       DATERANGE NOT NULL,
    computed_at       TIMESTAMPTZ NOT NULL,
    trigger_event_id  BIGINT,                     -- outbox row that caused this recompute
    PRIMARY KEY (employee_id, category_id, policy_id, valid_range)
);

CREATE INDEX idx_assignment_category ON assignment (category_id, valid_range);

-- Losers that must resurrect if the winning rule is deleted/narrowed.
CREATE TABLE shadowed_match (
    employee_id  TEXT NOT NULL,
    category_id  TEXT NOT NULL,
    rule_id      TEXT NOT NULL,
    by_rule_id   TEXT NOT NULL,
    valid_range  DATERANGE NOT NULL,
    computed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (employee_id, category_id, rule_id, valid_range)
);

-- ============================================================
-- Decision traces (immutable, written at decision time — the audit artifact)
-- ============================================================

CREATE TABLE decision_trace (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    employee_id     TEXT NOT NULL,
    category_id     TEXT NOT NULL,
    as_of_date      DATE  NOT NULL,
    trigger         TEXT NOT NULL CHECK (trigger IN
                        ('materialize', 'explain_query', 'preview', 'reconcile')),
    outcome         TEXT NOT NULL CHECK (outcome IN
                        ('assigned', 'shadowed', 'no_match', 'conflict_needs_decision')),
    facts_snapshot  JSONB NOT NULL,               -- attribute map used
    policy_snapshot JSONB NOT NULL,               -- category config + rule versions used
    evaluated       JSONB NOT NULL,               -- per-rule: matched?, why_not/why_lost, rank
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_trace_lookup ON decision_trace (employee_id, category_id, as_of_date);
CREATE INDEX idx_trace_time   ON decision_trace (created_at);   -- retention/aggregation sweeps

-- ============================================================
-- Transactional outbox (audit + push source; LISTEN/NOTIFY wakes workers)
-- ============================================================

CREATE TABLE outbox (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type      TEXT NOT NULL CHECK (event_type IN
                        ('fact_changed', 'rule_changed', 'category_changed', 'employee_changed')),
    company_id      TEXT NOT NULL,
    payload         JSONB NOT NULL,
    event_schema_version INT NOT NULL DEFAULT 1,  -- §22: versioned event payloads
    idempotency_key TEXT NOT NULL UNIQUE,
    recorded_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at    TIMESTAMPTZ,
    attempts        INT NOT NULL DEFAULT 0,
    dead_lettered   BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_outbox_unprocessed ON outbox (id) WHERE processed_at IS NULL AND NOT dead_lettered;

-- ============================================================
-- Inverted attribute index (Rippling 'Supergroup' pattern)
-- Reconciliation cost ∝ affected diff, never company size.
-- ============================================================

CREATE TABLE attribute_index (
    company_id    TEXT NOT NULL,
    attribute_key TEXT NOT NULL,
    value_hash    TEXT NOT NULL,                  -- normalized value
    employee_ids  TEXT[] NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, attribute_key, value_hash)
);

-- ============================================================
-- Seed: the 5 demo categories spanning the cardinality spectrum
-- ============================================================

INSERT INTO policy_category (id, display_name, cardinality, resolution_strategy, default_priority) VALUES
    ('manager',           'Manager',            'single', 'explicit_user_choice',   100),
    ('pay_schedule',      'Pay Schedule',       'single', 'priority_rank',           50),
    ('benefits_plan',     'Benefits Plan',      'single', 'explicit_user_choice',    30),
    ('app_access',        'App Access',         'many',   'additive',                 0),
    ('compliance_training','Compliance Training','many',  'additive',                 0),
    ('time_off_vacation', 'Vacation Policy',    'single', 'priority_rank',           40);

INSERT INTO attribute_definition (key, value_type, allowed_ops, description) VALUES
    ('location',        'string', '{eq,ne,in,not_in}',       'Work location (e.g. US-CA)'),
    ('department',      'string', '{eq,ne,in,not_in}',       'Department'),
    ('employment_type', 'enum',   '{eq,ne,in,not_in}',       'full_time | contractor | intern'),
    ('level',           'string', '{eq,ne,in,not_in}',       'Job level'),
    ('tenure_days',     'number', '{gte,lte,gt,lt}',         'Derived from hire_date at resolve time'),
    ('is_manager',      'bool',   '{eq}',                    'Reports-to graph position'),
    ('hire_date',       'date',   '{eq,gte,lte}',            'Original hire date');

COMMIT;
