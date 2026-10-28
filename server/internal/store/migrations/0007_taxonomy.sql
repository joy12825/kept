-- Milestone 1.3: taxonomy (correspondents, document types, tags, custom fields).
-- Names are unique per workspace, ignoring ASCII letter case. sort_key is the
-- pinyin sort key computed by the server (internal/pinyin).
-- Matching: algorithm none|any|all|literal|regex|fuzzy|auto (design doc 5.6).
CREATE TABLE correspondents (
    id               INTEGER PRIMARY KEY,
    workspace_id     INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name             TEXT    NOT NULL COLLATE NOCASE,
    sort_key         TEXT    NOT NULL,
    match            TEXT    NOT NULL DEFAULT '',
    algorithm        TEXT    NOT NULL DEFAULT 'auto'
                     CHECK (algorithm IN ('none', 'any', 'all', 'literal', 'regex', 'fuzzy', 'auto')),
    case_insensitive INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    UNIQUE (workspace_id, name)
);
CREATE INDEX correspondents_sort ON correspondents (workspace_id, sort_key);

CREATE TABLE document_types (
    id               INTEGER PRIMARY KEY,
    workspace_id     INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name             TEXT    NOT NULL COLLATE NOCASE,
    sort_key         TEXT    NOT NULL,
    match            TEXT    NOT NULL DEFAULT '',
    algorithm        TEXT    NOT NULL DEFAULT 'auto'
                     CHECK (algorithm IN ('none', 'any', 'all', 'literal', 'regex', 'fuzzy', 'auto')),
    case_insensitive INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    UNIQUE (workspace_id, name)
);
CREATE INDEX document_types_sort ON document_types (workspace_id, sort_key);

-- parent_id SET NULL is only a backstop: deleting a tag first moves its
-- children up to its own parent (the service does it in one transaction).
CREATE TABLE tags (
    id               INTEGER PRIMARY KEY,
    workspace_id     INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name             TEXT    NOT NULL COLLATE NOCASE,
    sort_key         TEXT    NOT NULL,
    color            TEXT    NOT NULL,
    parent_id        INTEGER REFERENCES tags (id) ON DELETE SET NULL,
    is_inbox         INTEGER NOT NULL DEFAULT 0,
    match            TEXT    NOT NULL DEFAULT '',
    algorithm        TEXT    NOT NULL DEFAULT 'auto'
                     CHECK (algorithm IN ('none', 'any', 'all', 'literal', 'regex', 'fuzzy', 'auto')),
    case_insensitive INTEGER NOT NULL DEFAULT 1,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    UNIQUE (workspace_id, name)
);
CREATE INDEX tags_sort ON tags (workspace_id, sort_key);
CREATE INDEX tags_parent ON tags (parent_id);

-- key is set only for built-in fields (amount, due_date), which cannot be deleted.
CREATE TABLE custom_fields (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    key          TEXT,
    name         TEXT    NOT NULL COLLATE NOCASE,
    sort_key     TEXT    NOT NULL,
    data_type    TEXT    NOT NULL
                 CHECK (data_type IN ('text', 'integer', 'float', 'boolean', 'date', 'url', 'monetary', 'select', 'document_link')),
    extra        TEXT    NOT NULL DEFAULT '{}',
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (workspace_id, name),
    UNIQUE (workspace_id, key)
);
CREATE INDEX custom_fields_sort ON custom_fields (workspace_id, sort_key);

-- Built-in fields for the workspaces that already exist. New workspaces get
-- them from taxonomy.SeedWorkspace. char() keeps this file ASCII-only:
-- char(0x91D1, 0x989D) is "jin e" (amount), char(0x5230, 0x671F, 0x65E5) is
-- "dao qi ri" (due date).
INSERT INTO custom_fields (workspace_id, key, name, sort_key, data_type, extra, created_at, updated_at)
SELECT id, 'amount', char(0x91D1, 0x989D), 'jine', 'monetary', '{"currency":"CNY"}', created_at, created_at FROM workspaces;
INSERT INTO custom_fields (workspace_id, key, name, sort_key, data_type, extra, created_at, updated_at)
SELECT id, 'due_date', char(0x5230, 0x671F, 0x65E5), 'daoqiri', 'date', '{}', created_at, created_at FROM workspaces;
