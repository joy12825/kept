-- Milestone 1.8: drawers, their facts and alerts, the yearbook flags and
-- read-only sharing (design doc 3.6).
--
-- A drawer's rule is filter JSON (11.9); '' means no rule, which files
-- nothing. Facts are ordered; the first is the drawer's primary fact.
CREATE TABLE drawers (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    name         TEXT    NOT NULL,
    icon         TEXT    NOT NULL DEFAULT 'file',
    color        TEXT    NOT NULL DEFAULT 'blue',
    description  TEXT    NOT NULL DEFAULT '',
    template_key TEXT,
    rule         TEXT    NOT NULL DEFAULT '',
    sort_order   INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (workspace_id, name)
);
CREATE INDEX drawers_workspace ON drawers (workspace_id, sort_order);

-- kind: latest | due | sum | expect | pinned; config is JSON per kind.
CREATE TABLE drawer_facts (
    id         INTEGER PRIMARY KEY,
    drawer_id  INTEGER NOT NULL REFERENCES drawers (id) ON DELETE CASCADE,
    kind       TEXT    NOT NULL CHECK (kind IN ('latest', 'due', 'sum', 'expect', 'pinned')),
    label      TEXT    NOT NULL,
    config     TEXT    NOT NULL DEFAULT '{}',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL
);
CREATE INDEX drawer_facts_drawer ON drawer_facts (drawer_id, sort_order);

-- One alert per fact and period (period_key, e.g. expect:2024-10 or
-- due:<document>:2025-03-02). A dismissed alert stays dismissed for its
-- period; one whose condition no longer holds becomes resolved.
CREATE TABLE alerts (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    drawer_id    INTEGER NOT NULL REFERENCES drawers (id) ON DELETE CASCADE,
    fact_id      INTEGER NOT NULL REFERENCES drawer_facts (id) ON DELETE CASCADE,
    kind         TEXT    NOT NULL CHECK (kind IN ('due', 'missing')),
    period_key   TEXT    NOT NULL,
    document_id  INTEGER REFERENCES documents (id) ON DELETE CASCADE,
    period_label TEXT    NOT NULL DEFAULT '',
    due_date     TEXT,
    status       TEXT    NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed', 'resolved')),
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    UNIQUE (fact_id, period_key)
);
CREATE INDEX alerts_workspace ON alerts (workspace_id, status);

CREATE TABLE drawer_shares (
    drawer_id           INTEGER NOT NULL REFERENCES drawers (id) ON DELETE CASCADE,
    target_workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    created_by          INTEGER REFERENCES users (id) ON DELETE SET NULL,
    created_at          INTEGER NOT NULL,
    PRIMARY KEY (drawer_id, target_workspace_id)
);
CREATE INDEX drawer_shares_target ON drawer_shares (target_workspace_id);

-- drawer_manual: a person chose the drawer (or chose none); rules leave
-- the document alone until it is handed back.
ALTER TABLE documents ADD COLUMN drawer_id INTEGER REFERENCES drawers (id) ON DELETE SET NULL;
ALTER TABLE documents ADD COLUMN drawer_manual INTEGER NOT NULL DEFAULT 0;
ALTER TABLE documents ADD COLUMN is_milestone INTEGER NOT NULL DEFAULT 0;
CREATE INDEX documents_drawer ON documents (workspace_id, drawer_id);

ALTER TABLE tags ADD COLUMN show_in_yearbook INTEGER NOT NULL DEFAULT 0;
