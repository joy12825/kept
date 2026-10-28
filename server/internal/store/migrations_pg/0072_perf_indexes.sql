-- v1.13.5: indexes for spaces with tens of thousands of documents (see the
-- SQLite migration); the same here, so both plan alike.
CREATE INDEX custom_field_values_doclink ON custom_field_values (value_doclink) WHERE value_doclink IS NOT NULL;
DROP INDEX documents_drawer;
CREATE INDEX documents_drawer ON documents (workspace_id, drawer_id, deleted_at, private, person_id, added_by, uploader_access);
CREATE INDEX documents_cover ON documents (workspace_id, deleted_at, private, person_id, added_by, uploader_access,
    document_type_id, correspondent_id, location_id, created_date, inbox, drawer_id);
