CREATE TABLE students (
    id           TEXT PRIMARY KEY,
    google_sub   TEXT NOT NULL UNIQUE,
    email        TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    lang_pref    TEXT NOT NULL DEFAULT 'hinglish',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE devices (
    id           TEXT PRIMARY KEY,
    student_id   TEXT REFERENCES students(id),
    lang_pref    TEXT NOT NULL DEFAULT 'hinglish',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE attempts (
    id              TEXT PRIMARY KEY,
    device_id       TEXT NOT NULL,
    student_id      TEXT,
    exercise_id     TEXT NOT NULL,
    mode            TEXT NOT NULL,
    idempotency_key TEXT NOT NULL DEFAULT '',
    code            TEXT NOT NULL,
    outcome         TEXT NOT NULL,
    passed          BOOLEAN NOT NULL,
    error_type      TEXT NOT NULL DEFAULT '',
    error_message   TEXT NOT NULL DEFAULT '',
    error_line      INT NOT NULL DEFAULT 0,
    failing_test    TEXT NOT NULL DEFAULT '',
    flags           TEXT[] NOT NULL DEFAULT '{}',
    result          JSONB NOT NULL,
    duration_ms     INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX attempts_device_exercise ON attempts (device_id, exercise_id, created_at DESC);
CREATE INDEX attempts_student_exercise ON attempts (student_id, exercise_id, created_at DESC) WHERE student_id IS NOT NULL;
CREATE UNIQUE INDEX attempts_idempotency ON attempts (device_id, idempotency_key) WHERE idempotency_key <> '';

CREATE TABLE progress (
    owner_kind      TEXT NOT NULL,
    owner_id        TEXT NOT NULL,
    exercise_id     TEXT NOT NULL,
    status          TEXT NOT NULL,
    best_attempt_id TEXT NOT NULL DEFAULT '',
    passed_at       TIMESTAMPTZ,
    PRIMARY KEY (owner_kind, owner_id, exercise_id)
);

CREATE TABLE hints (
    id          TEXT PRIMARY KEY,
    attempt_id  TEXT NOT NULL,
    owner_kind  TEXT NOT NULL,
    owner_id    TEXT NOT NULL,
    exercise_id TEXT NOT NULL,
    level       INT NOT NULL,
    source      TEXT NOT NULL,
    lang        TEXT NOT NULL,
    text        TEXT NOT NULL,
    line        INT NOT NULL DEFAULT 0,
    model       TEXT NOT NULL DEFAULT '',
    tokens      INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX hints_owner_exercise ON hints (owner_kind, owner_id, exercise_id, created_at DESC);

CREATE TABLE hint_cache (
    key        TEXT PRIMARY KEY,
    text       TEXT NOT NULL,
    lang       TEXT NOT NULL,
    model      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE usage_daily (
    day   TEXT NOT NULL,
    kind  TEXT NOT NULL,
    owner TEXT NOT NULL,
    count INT NOT NULL DEFAULT 0,
    PRIMARY KEY (day, kind, owner)
);
