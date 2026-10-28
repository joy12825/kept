-- v1.7.7: remote inboxes. A workspace administrator points a folder of a
-- storage (a NAS share, a cloud drive) at the workspace; files put there
-- are ingested, and a file changed afterwards becomes a new version.
CREATE TABLE remote_inboxes (
    workspace_id INTEGER PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    storage      TEXT    NOT NULL,
    folder       TEXT    NOT NULL,
    after        TEXT    NOT NULL DEFAULT 'keep' CHECK (after IN ('keep', 'move')),
    enabled      INTEGER NOT NULL DEFAULT 1,
    scanned_at   INTEGER,
    scan_error   TEXT    NOT NULL DEFAULT '',
    updated_by   INTEGER REFERENCES users (id) ON DELETE SET NULL,
    updated_at   INTEGER NOT NULL
);

-- Every file seen in an inbox. state: waiting (seen once, must stay the
-- same until the next scan), queued (submitted: job_id), imported
-- (document_id), duplicate (already a document: document_id), quota (the
-- workspace is full: tried again), failed (error; tried again only when
-- the file changes), moved (imported, then moved to _已入库/).
CREATE TABLE remote_inbox_files (
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    key          TEXT    NOT NULL,
    size         INTEGER NOT NULL,
    mod          INTEGER NOT NULL,
    state        TEXT    NOT NULL,
    job_id       INTEGER,
    document_id  INTEGER REFERENCES documents (id) ON DELETE SET NULL,
    error        TEXT    NOT NULL DEFAULT '',
    seen_at      INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, key)
);
