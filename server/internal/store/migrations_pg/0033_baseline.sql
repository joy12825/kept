-- Postgres baseline (v1.0 stage 38): the SQLite schema as of migration
-- 0033, converted (INTEGER -> BIGINT, REAL -> DOUBLE PRECISION, BLOB ->
-- BYTEA, TEXT COLLATE NOCASE -> CITEXT, TEXT -> TEXT COLLATE "C": SQLite
-- compares text bytewise), plus what the SQLite queries
-- expect: json_extract, instr, group_concat, kept_json_each, and ids that
-- default to the largest plus one, as SQLite's INTEGER PRIMARY KEY does.
-- The vector index (vec1 on SQLite) is a pgvector table.
-- New migrations are written for both databases (migrations/ and
-- migrations_pg/), with the same version numbers.

CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS vector;

CREATE FUNCTION json_extract(doc text, path text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE WHEN doc IS NULL OR doc = '' THEN NULL ELSE jsonb_path_query_first(doc::jsonb, path::jsonpath) #>> '{}' END
$$;

CREATE FUNCTION instr(haystack text, needle text) RETURNS integer LANGUAGE sql IMMUTABLE AS $$
  SELECT strpos(haystack, needle)
$$;

CREATE FUNCTION kept_concat_step(acc text, val anyelement, sep text) RETURNS text LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE WHEN val IS NULL THEN acc WHEN acc IS NULL THEN val::text ELSE acc || sep || val::text END
$$;
CREATE FUNCTION kept_concat_step(acc text, val anyelement) RETURNS text LANGUAGE sql IMMUTABLE AS $$
  SELECT CASE WHEN val IS NULL THEN acc WHEN acc IS NULL THEN val::text ELSE acc || ',' || val::text END
$$;
CREATE AGGREGATE group_concat(anyelement, text) (SFUNC = kept_concat_step, STYPE = text);
CREATE AGGREGATE group_concat(anyelement) (SFUNC = kept_concat_step, STYPE = text);

-- SQLite's json_each over an array: the index (key) and the element (value).
CREATE FUNCTION kept_json_each(doc text) RETURNS TABLE (key bigint, value text) LANGUAGE sql IMMUTABLE AS $$
  SELECT (t.ord - 1)::bigint, t.elem
  FROM jsonb_array_elements_text(CASE WHEN doc IS NULL OR doc = '' THEN '[]' ELSE doc END::jsonb) WITH ORDINALITY AS t(elem, ord)
$$;

-- An id left out becomes the largest so far plus one (SQLite's INTEGER
-- PRIMARY KEY); the transaction-level lock keeps two inserts apart.
CREATE FUNCTION kept_next_id() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.id IS NULL THEN
    PERFORM pg_advisory_xact_lock(hashtext(TG_TABLE_NAME));
    EXECUTE format('SELECT coalesce(max(id), 0) + 1 FROM %I', TG_TABLE_NAME) INTO NEW.id;
  END IF;
  RETURN NEW;
END
$$;

CREATE TABLE users (
    id                  BIGINT PRIMARY KEY,
    username            TEXT COLLATE "C"    NOT NULL UNIQUE,
    display_name        TEXT COLLATE "C"    NOT NULL DEFAULT '',
    email               TEXT COLLATE "C",
    password_hash       TEXT COLLATE "C"    NOT NULL,
    is_admin            BIGINT NOT NULL DEFAULT 0,
    disabled_at         BIGINT,
    password_changed_at BIGINT NOT NULL,
    created_at          BIGINT NOT NULL,
    updated_at          BIGINT NOT NULL
, sso_issuer TEXT COLLATE "C", sso_subject TEXT COLLATE "C");

CREATE TABLE IF NOT EXISTS "workspaces" (
    id         BIGINT PRIMARY KEY,
    name       TEXT COLLATE "C"    NOT NULL,
    kind       TEXT COLLATE "C"    NOT NULL CHECK (kind IN ('personal', 'family', 'team')),
    color      TEXT COLLATE "C"    NOT NULL DEFAULT 'blue',
    created_by BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at BIGINT NOT NULL,
    deleted_at BIGINT
, yearbook_rules TEXT COLLATE "C", ai_settings TEXT COLLATE "C" NOT NULL DEFAULT '{}', quota_bytes BIGINT);

CREATE TABLE workspace_members (
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role         TEXT COLLATE "C"    NOT NULL CHECK (role IN ('owner', 'editor', 'viewer')),
    created_at   BIGINT NOT NULL, notify_email BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (workspace_id, user_id)
);

CREATE TABLE sessions (
    id           TEXT COLLATE "C"    PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    csrf_token   TEXT COLLATE "C"    NOT NULL,
    ip           TEXT COLLATE "C"    NOT NULL DEFAULT '',
    user_agent   TEXT COLLATE "C"    NOT NULL DEFAULT '',
    created_at   BIGINT NOT NULL,
    last_seen_at BIGINT NOT NULL,
    expires_at   BIGINT NOT NULL
, persistent BIGINT NOT NULL DEFAULT 1);

CREATE TABLE api_tokens (
    id           BIGINT PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         TEXT COLLATE "C"    NOT NULL,
    token_hash   TEXT COLLATE "C"    NOT NULL UNIQUE,
    prefix       TEXT COLLATE "C"    NOT NULL,
    created_at   BIGINT NOT NULL,
    last_used_at BIGINT
, kind TEXT COLLATE "C" NOT NULL DEFAULT 'personal' CHECK (kind IN ('personal', 'device')), workspace_id BIGINT REFERENCES workspaces (id) ON DELETE CASCADE, device_name TEXT COLLATE "C" NOT NULL DEFAULT '', paired_at BIGINT);

CREATE TABLE settings (
    key   TEXT COLLATE "C" PRIMARY KEY,
    value TEXT COLLATE "C" NOT NULL
);

CREATE TABLE correspondents (
    id               BIGINT PRIMARY KEY,
    workspace_id     BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name             CITEXT    NOT NULL ,
    sort_key         TEXT COLLATE "C"    NOT NULL,
    match            TEXT COLLATE "C"    NOT NULL DEFAULT '',
    algorithm        TEXT COLLATE "C"    NOT NULL DEFAULT 'auto'
                     CHECK (algorithm IN ('none', 'any', 'all', 'literal', 'regex', 'fuzzy', 'auto')),
    case_insensitive BIGINT NOT NULL DEFAULT 1,
    created_at       BIGINT NOT NULL,
    updated_at       BIGINT NOT NULL,
    UNIQUE (workspace_id, name)
);

CREATE TABLE document_types (
    id               BIGINT PRIMARY KEY,
    workspace_id     BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name             CITEXT    NOT NULL ,
    sort_key         TEXT COLLATE "C"    NOT NULL,
    match            TEXT COLLATE "C"    NOT NULL DEFAULT '',
    algorithm        TEXT COLLATE "C"    NOT NULL DEFAULT 'auto'
                     CHECK (algorithm IN ('none', 'any', 'all', 'literal', 'regex', 'fuzzy', 'auto')),
    case_insensitive BIGINT NOT NULL DEFAULT 1,
    created_at       BIGINT NOT NULL,
    updated_at       BIGINT NOT NULL,
    UNIQUE (workspace_id, name)
);

CREATE TABLE drawers (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT COLLATE "C"    NOT NULL,
    icon         TEXT COLLATE "C"    NOT NULL DEFAULT 'file',
    color        TEXT COLLATE "C"    NOT NULL DEFAULT 'blue',
    description  TEXT COLLATE "C"    NOT NULL DEFAULT '',
    template_key TEXT COLLATE "C",
    rule         TEXT COLLATE "C"    NOT NULL DEFAULT '',
    sort_order   BIGINT NOT NULL DEFAULT 0,
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL, kind TEXT COLLATE "C" NOT NULL DEFAULT 'normal' CHECK (kind IN ('normal', 'library')),
    UNIQUE (workspace_id, name)
);

CREATE TABLE documents (
    id                BIGINT PRIMARY KEY,
    workspace_id      BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    title             TEXT COLLATE "C"    NOT NULL,
    content           TEXT COLLATE "C"    NOT NULL DEFAULT '',
    created_date      TEXT COLLATE "C"    NOT NULL,          -- document date, YYYY-MM-DD
    added_at          BIGINT NOT NULL,
    modified_at       BIGINT NOT NULL,
    correspondent_id  BIGINT REFERENCES correspondents (id) ON DELETE SET NULL,
    document_type_id  BIGINT REFERENCES document_types (id) ON DELETE SET NULL,
    asn               BIGINT,
    mime_type         TEXT COLLATE "C"    NOT NULL,
    original_filename TEXT COLLATE "C"    NOT NULL,
    original_ext      TEXT COLLATE "C"    NOT NULL,
    page_count        BIGINT NOT NULL DEFAULT 0,
    checksum          TEXT COLLATE "C"    NOT NULL,
    archive_checksum  TEXT COLLATE "C",
    has_archive       BIGINT NOT NULL DEFAULT 0,
    has_thumbnail     BIGINT NOT NULL DEFAULT 0,
    inbox             BIGINT NOT NULL DEFAULT 1,
    deleted_at        BIGINT,
    field_sources     TEXT COLLATE "C"    NOT NULL DEFAULT '{}', title_sort TEXT COLLATE "C" NOT NULL DEFAULT '', index_rev BIGINT NOT NULL DEFAULT 1, indexed_rev BIGINT NOT NULL DEFAULT 0, drawer_id BIGINT REFERENCES drawers (id) ON DELETE SET NULL, drawer_manual BIGINT NOT NULL DEFAULT 0, is_milestone BIGINT NOT NULL DEFAULT 0, source TEXT COLLATE "C" NOT NULL DEFAULT 'web', page_edits TEXT COLLATE "C" NOT NULL DEFAULT '', original_encrypted BIGINT NOT NULL DEFAULT 0, simhash BIGINT, simhash_rev BIGINT NOT NULL DEFAULT 0, embedded_rev BIGINT NOT NULL DEFAULT 0,
    UNIQUE (workspace_id, checksum),
    UNIQUE (workspace_id, asn)
);

CREATE TABLE IF NOT EXISTS "jobs" (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT REFERENCES workspaces (id) ON DELETE CASCADE,
    kind         TEXT COLLATE "C"    NOT NULL,
    payload      TEXT COLLATE "C"    NOT NULL DEFAULT '{}',
    status       TEXT COLLATE "C"    NOT NULL DEFAULT 'queued'
                 CHECK (status IN ('queued', 'running', 'succeeded', 'failed')),
    priority     BIGINT NOT NULL DEFAULT 0,
    attempts     BIGINT NOT NULL DEFAULT 0,
    max_attempts BIGINT NOT NULL DEFAULT 3,
    run_after    BIGINT NOT NULL,
    heartbeat_at BIGINT,
    error        TEXT COLLATE "C",
    result       TEXT COLLATE "C",
    created_at   BIGINT NOT NULL,
    started_at   BIGINT,
    finished_at  BIGINT
, document_id BIGINT REFERENCES documents (id) ON DELETE SET NULL, client_upload_id TEXT COLLATE "C");

CREATE TABLE user_totp (
    user_id        BIGINT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret         TEXT COLLATE "C"    NOT NULL,
    enabled_at     BIGINT,
    last_used_step BIGINT NOT NULL DEFAULT 0,
    created_at     BIGINT NOT NULL
);

CREATE TABLE user_recovery_codes (
    id        BIGINT PRIMARY KEY,
    user_id   BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash TEXT COLLATE "C"    NOT NULL,
    used_at   BIGINT
);

CREATE TABLE audit_events (
    id            BIGINT PRIMARY KEY,
    workspace_id  BIGINT REFERENCES workspaces (id) ON DELETE CASCADE,
    actor_user_id BIGINT REFERENCES users (id) ON DELETE SET NULL,
    actor_via     TEXT COLLATE "C"    NOT NULL CHECK (actor_via IN ('session', 'token', 'device', 'agent', 'system')),
    action        TEXT COLLATE "C"    NOT NULL,
    target_type   TEXT COLLATE "C"    NOT NULL DEFAULT '',
    target_id     TEXT COLLATE "C"    NOT NULL DEFAULT '',
    summary       TEXT COLLATE "C"    NOT NULL,
    changes       TEXT COLLATE "C"    NOT NULL DEFAULT '{}',
    request_id    TEXT COLLATE "C"    NOT NULL DEFAULT '',
    created_at    BIGINT NOT NULL
);

CREATE TABLE tags (
    id               BIGINT PRIMARY KEY,
    workspace_id     BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name             CITEXT    NOT NULL ,
    sort_key         TEXT COLLATE "C"    NOT NULL,
    color            TEXT COLLATE "C"    NOT NULL,
    parent_id        BIGINT REFERENCES tags (id) ON DELETE SET NULL,
    is_inbox         BIGINT NOT NULL DEFAULT 0,
    match            TEXT COLLATE "C"    NOT NULL DEFAULT '',
    algorithm        TEXT COLLATE "C"    NOT NULL DEFAULT 'auto'
                     CHECK (algorithm IN ('none', 'any', 'all', 'literal', 'regex', 'fuzzy', 'auto')),
    case_insensitive BIGINT NOT NULL DEFAULT 1,
    created_at       BIGINT NOT NULL,
    updated_at       BIGINT NOT NULL, show_in_yearbook BIGINT NOT NULL DEFAULT 0,
    UNIQUE (workspace_id, name)
);

CREATE TABLE custom_fields (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    key          TEXT COLLATE "C",
    name         CITEXT    NOT NULL ,
    sort_key     TEXT COLLATE "C"    NOT NULL,
    data_type    TEXT COLLATE "C"    NOT NULL
                 CHECK (data_type IN ('text', 'integer', 'float', 'boolean', 'date', 'url', 'monetary', 'select', 'document_link')),
    extra        TEXT COLLATE "C"    NOT NULL DEFAULT '{}',
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL,
    UNIQUE (workspace_id, name),
    UNIQUE (workspace_id, key)
);

CREATE TABLE document_tags (
    document_id BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    tag_id      BIGINT NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (document_id, tag_id)
);

CREATE TABLE custom_field_values (
    document_id    BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    field_id       BIGINT NOT NULL REFERENCES custom_fields (id) ON DELETE CASCADE,
    value_text     TEXT COLLATE "C",
    value_int      BIGINT,
    value_float    DOUBLE PRECISION,
    value_bool     BIGINT,
    value_date     TEXT COLLATE "C",
    value_monetary BIGINT,
    value_select   TEXT COLLATE "C",
    value_doclink  BIGINT REFERENCES documents (id) ON DELETE SET NULL,
    PRIMARY KEY (document_id, field_id)
);

CREATE TABLE notes (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id  BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    user_id      BIGINT REFERENCES users (id) ON DELETE SET NULL,
    body         TEXT COLLATE "C"    NOT NULL,
    created_at   BIGINT NOT NULL
);

CREATE TABLE saved_views (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         CITEXT    NOT NULL ,
    sort_key     TEXT COLLATE "C"    NOT NULL,
    filter       TEXT COLLATE "C"    NOT NULL,
    sort         TEXT COLLATE "C"    NOT NULL DEFAULT '',
    created_by   BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL, display TEXT COLLATE "C" NOT NULL DEFAULT '{}',
    UNIQUE (workspace_id, name)
);

CREATE TABLE schedules (
    name        TEXT COLLATE "C"    PRIMARY KEY,
    next_run_at BIGINT NOT NULL,
    last_run_at BIGINT
);

CREATE TABLE classifier_models (
    workspace_id   BIGINT PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    format_version BIGINT NOT NULL,
    trained_at     BIGINT NOT NULL,
    data_hash      TEXT COLLATE "C"    NOT NULL,
    doc_count      BIGINT NOT NULL,
    summary        TEXT COLLATE "C"    NOT NULL DEFAULT '{}',
    blob           BYTEA
);

CREATE TABLE drawer_facts (
    id         BIGINT PRIMARY KEY,
    drawer_id  BIGINT NOT NULL REFERENCES drawers (id) ON DELETE CASCADE,
    kind       TEXT COLLATE "C"    NOT NULL CHECK (kind IN ('latest', 'due', 'sum', 'expect', 'pinned')),
    label      TEXT COLLATE "C"    NOT NULL,
    config     TEXT COLLATE "C"    NOT NULL DEFAULT '{}',
    sort_order BIGINT NOT NULL DEFAULT 0,
    created_at BIGINT NOT NULL
);

CREATE TABLE alerts (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    drawer_id    BIGINT NOT NULL REFERENCES drawers (id) ON DELETE CASCADE,
    fact_id      BIGINT NOT NULL REFERENCES drawer_facts (id) ON DELETE CASCADE,
    kind         TEXT COLLATE "C"    NOT NULL CHECK (kind IN ('due', 'missing')),
    period_key   TEXT COLLATE "C"    NOT NULL,
    document_id  BIGINT REFERENCES documents (id) ON DELETE CASCADE,
    period_label TEXT COLLATE "C"    NOT NULL DEFAULT '',
    due_date     TEXT COLLATE "C",
    status       TEXT COLLATE "C"    NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed', 'resolved')),
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL, emailed_at BIGINT, note TEXT COLLATE "C" NOT NULL DEFAULT '', note_run BIGINT,
    UNIQUE (fact_id, period_key)
);

CREATE TABLE drawer_shares (
    drawer_id           BIGINT NOT NULL REFERENCES drawers (id) ON DELETE CASCADE,
    target_workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    created_by          BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at          BIGINT NOT NULL,
    PRIMARY KEY (drawer_id, target_workspace_id)
);

CREATE TABLE pairing_codes (
    id           BIGINT PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    code_hash    TEXT COLLATE "C"    NOT NULL UNIQUE,
    created_at   BIGINT NOT NULL,
    expires_at   BIGINT NOT NULL,
    used_at      BIGINT
);

CREATE TABLE mail_accounts (
    id               BIGINT PRIMARY KEY,
    workspace_id     BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name             TEXT COLLATE "C"    NOT NULL,
    host             TEXT COLLATE "C"    NOT NULL,
    port             BIGINT NOT NULL,
    security         TEXT COLLATE "C"    NOT NULL DEFAULT 'tls' CHECK (security IN ('tls', 'starttls', 'none')),
    username         TEXT COLLATE "C"    NOT NULL,
    password         TEXT COLLATE "C"    NOT NULL,
    folder           TEXT COLLATE "C"    NOT NULL DEFAULT 'INBOX',
    filter_from      TEXT COLLATE "C"    NOT NULL DEFAULT '',
    filter_subject   TEXT COLLATE "C"    NOT NULL DEFAULT '',
    document_type_id BIGINT REFERENCES document_types (id) ON DELETE SET NULL,
    correspondent_id BIGINT REFERENCES correspondents (id) ON DELETE SET NULL,
    tag_ids          TEXT COLLATE "C"    NOT NULL DEFAULT '[]',
    after_action     TEXT COLLATE "C"    NOT NULL DEFAULT 'read' CHECK (after_action IN ('read', 'move')),
    move_to          TEXT COLLATE "C"    NOT NULL DEFAULT '',
    enabled          BIGINT NOT NULL DEFAULT 1,
    checked_at       BIGINT,
    last_error       TEXT COLLATE "C"    NOT NULL DEFAULT '',
    last_count       BIGINT NOT NULL DEFAULT 0,
    created_at       BIGINT NOT NULL,
    updated_at       BIGINT NOT NULL
);

CREATE TABLE mail_seen (
    account_id   BIGINT NOT NULL REFERENCES mail_accounts (id) ON DELETE CASCADE,
    uid_validity BIGINT NOT NULL,
    uid          BIGINT NOT NULL,
    seen_at      BIGINT NOT NULL,
    PRIMARY KEY (account_id, uid_validity, uid)
);

CREATE TABLE workflows (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT COLLATE "C"    NOT NULL,
    enabled      BIGINT NOT NULL DEFAULT 1,
    sort_order   BIGINT NOT NULL DEFAULT 0,
    triggers     TEXT COLLATE "C"    NOT NULL DEFAULT '[]',
    conditions   TEXT COLLATE "C"    NOT NULL DEFAULT '{}',
    actions      TEXT COLLATE "C"    NOT NULL DEFAULT '[]',
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL,
    UNIQUE (workspace_id, name)
);

CREATE TABLE workflow_runs (
    id           BIGINT PRIMARY KEY,
    workflow_id  BIGINT NOT NULL REFERENCES workflows (id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id  BIGINT REFERENCES documents (id) ON DELETE SET NULL,
    trigger      TEXT COLLATE "C"    NOT NULL CHECK (trigger IN ('consumption', 'added', 'updated', 'scheduled')),
    status       TEXT COLLATE "C"    NOT NULL CHECK (status IN ('ok', 'failed')),
    message      TEXT COLLATE "C"    NOT NULL DEFAULT '',
    created_at   BIGINT NOT NULL
);

CREATE TABLE share_links (
    id             BIGINT PRIMARY KEY,
    workspace_id   BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id    BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    token          TEXT COLLATE "C"    NOT NULL UNIQUE,
    file           TEXT COLLATE "C"    NOT NULL CHECK (file IN ('archive', 'original')),
    expires_at     BIGINT NOT NULL,
    created_by     BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at     BIGINT NOT NULL,
    revoked_at     BIGINT,
    access_count   BIGINT NOT NULL DEFAULT 0,
    last_access_at BIGINT
);

CREATE TABLE document_versions (
    id                BIGINT PRIMARY KEY,
    workspace_id      BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id       BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    number            BIGINT NOT NULL,
    label             TEXT COLLATE "C"    NOT NULL DEFAULT '',
    original_filename TEXT COLLATE "C"    NOT NULL,
    original_ext      TEXT COLLATE "C"    NOT NULL,
    mime_type         TEXT COLLATE "C"    NOT NULL,
    checksum          TEXT COLLATE "C"    NOT NULL,
    size              BIGINT NOT NULL,
    encrypted         BIGINT NOT NULL DEFAULT 0,
    current           BIGINT NOT NULL DEFAULT 0,
    created_by        BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at        BIGINT NOT NULL,
    UNIQUE (document_id, number)
);

CREATE TABLE duplicate_pairs (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    doc_a        BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    doc_b        BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    distance     BIGINT NOT NULL,
    status       TEXT COLLATE "C"    NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed')),
    found_at     BIGINT NOT NULL,
    CHECK (doc_a < doc_b),
    UNIQUE (doc_a, doc_b)
);

CREATE TABLE agent_runs (
    id            BIGINT PRIMARY KEY,
    workspace_id  BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    agent         TEXT COLLATE "C"    NOT NULL,
    trigger       TEXT COLLATE "C"    NOT NULL,
    document_id   BIGINT REFERENCES documents (id) ON DELETE SET NULL,
    model         TEXT COLLATE "C"    NOT NULL DEFAULT '',
    status        TEXT COLLATE "C"    NOT NULL CHECK (status IN ('running', 'done', 'failed', 'skipped')),
    calls         BIGINT NOT NULL DEFAULT 0,
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    duration_ms   BIGINT NOT NULL DEFAULT 0,
    summary       TEXT COLLATE "C"    NOT NULL DEFAULT '',
    error         TEXT COLLATE "C"    NOT NULL DEFAULT '',
    created_at    BIGINT NOT NULL
);

CREATE TABLE agent_steps (
    id     BIGINT PRIMARY KEY,
    run_id BIGINT NOT NULL REFERENCES agent_runs (id) ON DELETE CASCADE,
    seq    BIGINT NOT NULL,
    kind   TEXT COLLATE "C"    NOT NULL,
    input  TEXT COLLATE "C"    NOT NULL,
    output TEXT COLLATE "C"    NOT NULL
);

CREATE TABLE agent_proposals (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    run_id       BIGINT REFERENCES agent_runs (id) ON DELETE SET NULL,
    agent        TEXT COLLATE "C"    NOT NULL,
    kind         TEXT COLLATE "C"    NOT NULL,
    document_id  BIGINT REFERENCES documents (id) ON DELETE CASCADE,
    payload      TEXT COLLATE "C"    NOT NULL,
    confidence   DOUBLE PRECISION    NOT NULL DEFAULT 0,
    reason       TEXT COLLATE "C"    NOT NULL DEFAULT '',
    status       TEXT COLLATE "C"    NOT NULL CHECK (status IN ('applied', 'pending', 'accepted', 'rejected', 'reverted', 'expired')),
    decided_by   BIGINT REFERENCES users (id) ON DELETE SET NULL,
    decided_at   BIGINT,
    created_at   BIGINT NOT NULL
);

CREATE TABLE books (
    document_id  BIGINT PRIMARY KEY REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    isbn         TEXT COLLATE "C"    NOT NULL DEFAULT '',
    authors      TEXT COLLATE "C"    NOT NULL DEFAULT '[]',
    publisher    TEXT COLLATE "C"    NOT NULL DEFAULT '',
    series       TEXT COLLATE "C"    NOT NULL DEFAULT '',
    edition      TEXT COLLATE "C"    NOT NULL DEFAULT '',
    published    TEXT COLLATE "C"    NOT NULL DEFAULT '',
    pages        BIGINT NOT NULL DEFAULT 0,
    language     TEXT COLLATE "C"    NOT NULL DEFAULT '',
    description  TEXT COLLATE "C"    NOT NULL DEFAULT '',
    cover        BIGINT NOT NULL DEFAULT 0,
    updated_at   BIGINT NOT NULL
);

CREATE TABLE reading_progress (
    user_id      BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    document_id  BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    position     TEXT COLLATE "C" NOT NULL DEFAULT '',
    percent      DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at   BIGINT NOT NULL, status TEXT COLLATE "C" NOT NULL DEFAULT '' CHECK (status IN ('', 'want', 'reading', 'finished')), rating BIGINT NOT NULL DEFAULT 0 CHECK (rating BETWEEN 0 AND 5), started_on TEXT COLLATE "C" NOT NULL DEFAULT '', finished_on TEXT COLLATE "C" NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, document_id)
);

CREATE TABLE highlights (
    id           BIGINT PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    document_id  BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    locator      TEXT COLLATE "C" NOT NULL,
    quote        TEXT COLLATE "C" NOT NULL DEFAULT '',
    note         TEXT COLLATE "C" NOT NULL DEFAULT '',
    color        TEXT COLLATE "C" NOT NULL DEFAULT 'yellow' CHECK (color IN ('yellow', 'green', 'blue', 'pink')),
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL
);

CREATE TABLE book_lists (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT COLLATE "C" NOT NULL,
    description  TEXT COLLATE "C" NOT NULL DEFAULT '',
    created_by   BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL
);

CREATE TABLE book_list_items (
    list_id     BIGINT NOT NULL REFERENCES book_lists (id) ON DELETE CASCADE,
    document_id BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    position    BIGINT NOT NULL,
    note        TEXT COLLATE "C" NOT NULL DEFAULT '',
    added_at    BIGINT NOT NULL,
    PRIMARY KEY (list_id, document_id)
);

CREATE TABLE doc_chunks (
    id           BIGINT PRIMARY KEY,
    document_id  BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    seq          BIGINT NOT NULL,
    text         TEXT COLLATE "C" NOT NULL,
    hash         TEXT COLLATE "C" NOT NULL
);

CREATE TABLE qa_conversations (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title        TEXT COLLATE "C" NOT NULL,
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL
);

CREATE TABLE qa_messages (
    id              BIGINT PRIMARY KEY,
    conversation_id BIGINT NOT NULL REFERENCES qa_conversations (id) ON DELETE CASCADE,
    role            TEXT COLLATE "C" NOT NULL CHECK (role IN ('user', 'assistant')),
    content         TEXT COLLATE "C" NOT NULL,
    sources         TEXT COLLATE "C" NOT NULL DEFAULT '[]',
    status          TEXT COLLATE "C" NOT NULL DEFAULT 'done' CHECK (status IN ('done', 'stopped', 'failed')),
    run_id          BIGINT REFERENCES agent_runs (id) ON DELETE SET NULL,
    created_at      BIGINT NOT NULL
);

CREATE TABLE yearbook_notes (
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    year         BIGINT NOT NULL,
    text         TEXT COLLATE "C"    NOT NULL,
    basis        TEXT COLLATE "C"    NOT NULL,
    run_id       BIGINT,
    created_at   BIGINT NOT NULL,
    PRIMARY KEY (workspace_id, year)
);

CREATE TABLE book_summaries (
    document_id  BIGINT PRIMARY KEY REFERENCES documents (id) ON DELETE CASCADE,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    summary      TEXT COLLATE "C"    NOT NULL,
    chapters     TEXT COLLATE "C"    NOT NULL DEFAULT '[]',
    basis        TEXT COLLATE "C"    NOT NULL,
    run_id       BIGINT,
    created_at   BIGINT NOT NULL
);

CREATE TABLE review_cards (
    id            BIGINT PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    workspace_id  BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    document_id   BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    highlight_id  BIGINT REFERENCES highlights (id) ON DELETE SET NULL,
    front         TEXT COLLATE "C"    NOT NULL,
    back          TEXT COLLATE "C"    NOT NULL,
    due_on        TEXT COLLATE "C"    NOT NULL,
    interval_days BIGINT NOT NULL DEFAULT 0,
    reps          BIGINT NOT NULL DEFAULT 0,
    lapses        BIGINT NOT NULL DEFAULT 0,
    created_at    BIGINT NOT NULL,
    updated_at    BIGINT NOT NULL
);

CREATE TABLE collections (
    id           BIGINT PRIMARY KEY,
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT COLLATE "C"    NOT NULL,
    kind         TEXT COLLATE "C"    NOT NULL CHECK (kind IN ('documents', 'filter')),
    filter       TEXT COLLATE "C"    NOT NULL DEFAULT '',
    created_by   BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at   BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL
);

CREATE TABLE collection_documents (
    collection_id BIGINT NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    document_id   BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    PRIMARY KEY (collection_id, document_id)
);

CREATE TABLE collection_shares (
    collection_id       BIGINT NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    target_workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    created_by          BIGINT REFERENCES users (id) ON DELETE SET NULL,
    created_at          BIGINT NOT NULL,
    PRIMARY KEY (collection_id, target_workspace_id)
);

-- Vectors of the chunks (doc_chunks.id = id); one dimension per model.
CREATE TABLE chunk_vectors (
    id           BIGINT PRIMARY KEY,
    vector       vector NOT NULL,
    workspace_id BIGINT NOT NULL
);
CREATE INDEX chunk_vectors_workspace ON chunk_vectors (workspace_id);

CREATE INDEX workspace_members_user ON workspace_members (user_id);
CREATE INDEX sessions_user ON sessions (user_id);
CREATE INDEX sessions_expires ON sessions (expires_at);
CREATE INDEX api_tokens_user ON api_tokens (user_id);
CREATE INDEX jobs_ready ON jobs (status, priority, run_after);
CREATE INDEX jobs_workspace ON jobs (workspace_id, created_at);
CREATE UNIQUE INDEX users_email ON users (email) WHERE email IS NOT NULL;
CREATE INDEX user_recovery_codes_user ON user_recovery_codes (user_id);
CREATE INDEX audit_events_workspace ON audit_events (workspace_id, id);
CREATE INDEX audit_events_created ON audit_events (created_at);
CREATE INDEX correspondents_sort ON correspondents (workspace_id, sort_key);
CREATE INDEX document_types_sort ON document_types (workspace_id, sort_key);
CREATE INDEX tags_sort ON tags (workspace_id, sort_key);
CREATE INDEX tags_parent ON tags (parent_id);
CREATE INDEX custom_fields_sort ON custom_fields (workspace_id, sort_key);
CREATE INDEX documents_inbox ON documents (workspace_id, inbox, added_at);
CREATE INDEX documents_date ON documents (workspace_id, created_date);
CREATE INDEX documents_correspondent ON documents (correspondent_id);
CREATE INDEX documents_type ON documents (document_type_id);
CREATE INDEX document_tags_tag ON document_tags (tag_id);
CREATE INDEX custom_field_values_field ON custom_field_values (field_id);
CREATE INDEX notes_document ON notes (document_id, created_at);
CREATE INDEX documents_title ON documents (workspace_id, title_sort);
CREATE INDEX documents_added ON documents (workspace_id, added_at);
CREATE INDEX documents_deleted ON documents (workspace_id, deleted_at);
CREATE INDEX audit_events_target ON audit_events (workspace_id, target_type, target_id, id);
CREATE INDEX documents_index_behind ON documents (id) WHERE indexed_rev < index_rev;
CREATE INDEX drawers_workspace ON drawers (workspace_id, sort_order);
CREATE INDEX drawer_facts_drawer ON drawer_facts (drawer_id, sort_order);
CREATE INDEX alerts_workspace ON alerts (workspace_id, status);
CREATE INDEX drawer_shares_target ON drawer_shares (target_workspace_id);
CREATE INDEX documents_drawer ON documents (workspace_id, drawer_id);
CREATE INDEX pairing_codes_user ON pairing_codes (user_id, workspace_id);
CREATE UNIQUE INDEX jobs_client_upload ON jobs (workspace_id, client_upload_id) WHERE client_upload_id IS NOT NULL;
CREATE INDEX mail_accounts_workspace ON mail_accounts (workspace_id);
CREATE INDEX workflows_workspace ON workflows (workspace_id, sort_order);
CREATE INDEX workflow_runs_workflow ON workflow_runs (workflow_id, id);
CREATE INDEX workflow_runs_document ON workflow_runs (workflow_id, document_id, trigger);
CREATE INDEX share_links_document ON share_links (workspace_id, document_id);
CREATE INDEX document_versions_document ON document_versions (workspace_id, document_id);
CREATE INDEX duplicate_pairs_workspace ON duplicate_pairs (workspace_id, status);
CREATE INDEX agent_runs_workspace ON agent_runs (workspace_id, created_at);
CREATE INDEX agent_steps_run ON agent_steps (run_id, seq);
CREATE INDEX agent_proposals_document ON agent_proposals (workspace_id, document_id, status);
CREATE UNIQUE INDEX books_isbn ON books (workspace_id, isbn) WHERE isbn != '';
CREATE INDEX reading_progress_document ON reading_progress (document_id);
CREATE INDEX reading_progress_user ON reading_progress (user_id, workspace_id, status);
CREATE INDEX highlights_user_document ON highlights (user_id, document_id);
CREATE INDEX highlights_user_workspace ON highlights (user_id, workspace_id, id);
CREATE UNIQUE INDEX book_lists_name ON book_lists (workspace_id, name);
CREATE INDEX book_list_items_document ON book_list_items (document_id);
CREATE INDEX doc_chunks_document ON doc_chunks (document_id, seq);
CREATE INDEX doc_chunks_hash ON doc_chunks (workspace_id, hash);
CREATE INDEX qa_conversations_user ON qa_conversations (user_id, workspace_id, updated_at);
CREATE INDEX qa_messages_conversation ON qa_messages (conversation_id, id);
CREATE INDEX review_cards_user ON review_cards (user_id, workspace_id, due_on);
CREATE UNIQUE INDEX review_cards_highlight ON review_cards (highlight_id) WHERE highlight_id IS NOT NULL;
CREATE UNIQUE INDEX users_sso ON users (sso_issuer, sso_subject) WHERE sso_subject IS NOT NULL;
CREATE INDEX collections_workspace ON collections (workspace_id);
CREATE INDEX collection_documents_document ON collection_documents (document_id);
CREATE INDEX collection_shares_target ON collection_shares (target_workspace_id);

CREATE TRIGGER users_id BEFORE INSERT ON "users" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER api_tokens_id BEFORE INSERT ON "api_tokens" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER jobs_id BEFORE INSERT ON "jobs" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER user_recovery_codes_id BEFORE INSERT ON "user_recovery_codes" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER workspaces_id BEFORE INSERT ON "workspaces" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER audit_events_id BEFORE INSERT ON "audit_events" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER correspondents_id BEFORE INSERT ON "correspondents" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER document_types_id BEFORE INSERT ON "document_types" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER tags_id BEFORE INSERT ON "tags" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER custom_fields_id BEFORE INSERT ON "custom_fields" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER documents_id BEFORE INSERT ON "documents" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER notes_id BEFORE INSERT ON "notes" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER saved_views_id BEFORE INSERT ON "saved_views" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER drawers_id BEFORE INSERT ON "drawers" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER drawer_facts_id BEFORE INSERT ON "drawer_facts" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER alerts_id BEFORE INSERT ON "alerts" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER pairing_codes_id BEFORE INSERT ON "pairing_codes" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER mail_accounts_id BEFORE INSERT ON "mail_accounts" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER workflows_id BEFORE INSERT ON "workflows" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER workflow_runs_id BEFORE INSERT ON "workflow_runs" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER share_links_id BEFORE INSERT ON "share_links" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER document_versions_id BEFORE INSERT ON "document_versions" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER duplicate_pairs_id BEFORE INSERT ON "duplicate_pairs" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER agent_runs_id BEFORE INSERT ON "agent_runs" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER agent_steps_id BEFORE INSERT ON "agent_steps" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER agent_proposals_id BEFORE INSERT ON "agent_proposals" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER highlights_id BEFORE INSERT ON "highlights" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER book_lists_id BEFORE INSERT ON "book_lists" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER doc_chunks_id BEFORE INSERT ON "doc_chunks" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER qa_conversations_id BEFORE INSERT ON "qa_conversations" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER qa_messages_id BEFORE INSERT ON "qa_messages" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER review_cards_id BEFORE INSERT ON "review_cards" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
CREATE TRIGGER collections_id BEFORE INSERT ON "collections" FOR EACH ROW EXECUTE FUNCTION kept_next_id();
