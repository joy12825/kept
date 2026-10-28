-- v1.7.7: remote inboxes. A workspace administrator points a folder of a
-- storage (a NAS share, a cloud drive) at the workspace; files put there
-- are ingested, and a file changed afterwards becomes a new version.
CREATE TABLE remote_inboxes (
    workspace_id BIGINT PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    storage      TEXT COLLATE "C" NOT NULL,
    folder       TEXT COLLATE "C" NOT NULL,
    after        TEXT COLLATE "C" NOT NULL DEFAULT 'keep' CHECK (after IN ('keep', 'move')),
    enabled      BIGINT NOT NULL DEFAULT 1,
    scanned_at   BIGINT,
    scan_error   TEXT COLLATE "C" NOT NULL DEFAULT '',
    updated_by   BIGINT REFERENCES users (id) ON DELETE SET NULL,
    updated_at   BIGINT NOT NULL
);

-- Every file seen in an inbox. state: waiting (seen once, must stay the
-- same until the next scan), queued (submitted: job_id), imported
-- (document_id), duplicate (already a document: document_id), quota (the
-- workspace is full: tried again), failed (error; tried again only when
-- the file changes), moved (imported, then moved to _已入库/).
CREATE TABLE remote_inbox_files (
    workspace_id BIGINT NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    key          TEXT COLLATE "C" NOT NULL,
    size         BIGINT NOT NULL,
    mod          BIGINT NOT NULL,
    state        TEXT COLLATE "C" NOT NULL,
    job_id       BIGINT,
    document_id  BIGINT REFERENCES documents (id) ON DELETE SET NULL,
    error        TEXT COLLATE "C" NOT NULL DEFAULT '',
    seen_at      BIGINT NOT NULL,
    updated_at   BIGINT NOT NULL,
    PRIMARY KEY (workspace_id, key)
);
