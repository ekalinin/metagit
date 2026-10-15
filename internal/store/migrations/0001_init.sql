-- Times are unix milliseconds. NULL means "not set".

CREATE TABLE owner (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    name          TEXT    NOT NULL,
    password_hash TEXT    NOT NULL,
    created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
    id_hash    TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);

CREATE TABLE access_tokens (
    id           INTEGER PRIMARY KEY,
    name         TEXT    NOT NULL,
    token_hash   TEXT    NOT NULL UNIQUE,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER
);

CREATE TABLE ssh_keys (
    id          INTEGER PRIMARY KEY,
    name        TEXT    NOT NULL,
    public_key  TEXT    NOT NULL,
    fingerprint TEXT    NOT NULL UNIQUE,
    created_at  INTEGER NOT NULL
);

CREATE TABLE projects (
    id         INTEGER PRIMARY KEY,
    name       TEXT    NOT NULL UNIQUE,
    created_at INTEGER NOT NULL
);

CREATE TABLE protected_refs (
    id         INTEGER PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    pattern    TEXT    NOT NULL,
    UNIQUE (project_id, pattern)
);

CREATE TABLE targets (
    id              INTEGER PRIMARY KEY,
    project_id      INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name            TEXT    NOT NULL,
    url             TEXT    NOT NULL,
    auth_kind       TEXT    NOT NULL,
    username        TEXT    NOT NULL DEFAULT '',
    secret          BLOB,
    enabled         INTEGER NOT NULL DEFAULT 1,
    state           TEXT    NOT NULL DEFAULT 'pending',
    last_error      TEXT    NOT NULL DEFAULT '',
    last_success_at INTEGER,
    next_attempt_at INTEGER,
    attempt         INTEGER NOT NULL DEFAULT 0,
    created_at      INTEGER NOT NULL,
    UNIQUE (project_id, name)
);

-- What metagit last delivered to (or confirmed on) a target.
CREATE TABLE target_refs (
    target_id INTEGER NOT NULL REFERENCES targets (id) ON DELETE CASCADE,
    ref       TEXT    NOT NULL,
    object    TEXT    NOT NULL,
    PRIMARY KEY (target_id, ref)
);

CREATE TABLE archive_intents (
    id         INTEGER PRIMARY KEY,
    project_id INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    push_id    TEXT    NOT NULL,
    ref        TEXT    NOT NULL,
    old_object TEXT    NOT NULL,
    new_object TEXT    NOT NULL,
    done       INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);

CREATE INDEX archive_intents_pending ON archive_intents (push_id) WHERE done = 0;

-- target_id has no foreign key: the archive outlives a deleted target.
CREATE TABLE archive_refs (
    id          INTEGER PRIMARY KEY,
    project_id  INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    source      TEXT    NOT NULL,
    target_id   INTEGER,
    target_name TEXT    NOT NULL DEFAULT '',
    orig_ref    TEXT    NOT NULL,
    archive_ref TEXT    NOT NULL,
    object      TEXT    NOT NULL,
    created_at  INTEGER NOT NULL
);

CREATE INDEX archive_refs_project ON archive_refs (project_id, id);

CREATE TABLE events (
    id          INTEGER PRIMARY KEY,
    project_id  INTEGER NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    target_id   INTEGER,
    target_name TEXT    NOT NULL DEFAULT '',
    kind        TEXT    NOT NULL,
    ref         TEXT    NOT NULL DEFAULT '',
    archive_ref TEXT    NOT NULL DEFAULT '',
    message     TEXT    NOT NULL,
    created_at  INTEGER NOT NULL
);

CREATE INDEX events_project ON events (project_id, id);
