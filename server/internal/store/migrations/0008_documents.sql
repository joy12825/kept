-- Milestone 1.4: documents. File paths derive from (workspace_id, id) and
-- are not stored. inbox = 1 while the document waits in the inbox
-- ("dai zheng li"); finishing it clears the flag (milestone 1.5).
CREATE TABLE documents (
    id                INTEGER PRIMARY KEY,
    workspace_id      INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    title             TEXT    NOT NULL,
    content           TEXT    NOT NULL DEFAULT '',
    created_date      TEXT    NOT NULL,          -- document date, YYYY-MM-DD
    added_at          INTEGER NOT NULL,
    modified_at       INTEGER NOT NULL,
    correspondent_id  INTEGER REFERENCES correspondents (id) ON DELETE SET NULL,
    document_type_id  INTEGER REFERENCES document_types (id) ON DELETE SET NULL,
    asn               INTEGER,
    mime_type         TEXT    NOT NULL,
    original_filename TEXT    NOT NULL,
    original_ext      TEXT    NOT NULL,
    page_count        INTEGER NOT NULL DEFAULT 0,
    checksum          TEXT    NOT NULL,
    archive_checksum  TEXT,
    has_archive       INTEGER NOT NULL DEFAULT 0,
    has_thumbnail     INTEGER NOT NULL DEFAULT 0,
    inbox             INTEGER NOT NULL DEFAULT 1,
    deleted_at        INTEGER,
    field_sources     TEXT    NOT NULL DEFAULT '{}',
    UNIQUE (workspace_id, checksum),
    UNIQUE (workspace_id, asn)
);
CREATE INDEX documents_inbox ON documents (workspace_id, inbox, added_at);
CREATE INDEX documents_date ON documents (workspace_id, created_date);
CREATE INDEX documents_correspondent ON documents (correspondent_id);
CREATE INDEX documents_type ON documents (document_type_id);

CREATE TABLE document_tags (
    document_id INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    tag_id      INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (document_id, tag_id)
);
CREATE INDEX document_tags_tag ON document_tags (tag_id);

-- One column per data type so fields can be filtered and sorted in SQL
-- (design doc 3.4). value_monetary is in minor units (fen).
CREATE TABLE custom_field_values (
    document_id    INTEGER NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    field_id       INTEGER NOT NULL REFERENCES custom_fields (id) ON DELETE CASCADE,
    value_text     TEXT,
    value_int      INTEGER,
    value_float    REAL,
    value_bool     INTEGER,
    value_date     TEXT,
    value_monetary INTEGER,
    value_select   TEXT,
    value_doclink  INTEGER REFERENCES documents (id) ON DELETE SET NULL,
    PRIMARY KEY (document_id, field_id)
);
CREATE INDEX custom_field_values_field ON custom_field_values (field_id);

-- The document an ingest job produced, for the upload tray to link to.
ALTER TABLE jobs ADD COLUMN document_id INTEGER REFERENCES documents (id) ON DELETE SET NULL;
