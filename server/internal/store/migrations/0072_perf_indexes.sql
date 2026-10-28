-- v1.13.5: indexes for spaces with tens of thousands of documents.
--
-- A document's detail page lists the documents that link to it (a
-- "document" custom field); without an index that scanned every field value
-- for each page (100 ms at 50,000 documents, and exporting a space took
-- minutes).
CREATE INDEX custom_field_values_doclink ON custom_field_values (value_doclink) WHERE value_doclink IS NOT NULL;
-- Counting a space's documents (the list's total, drawer counts, the
-- sidebar's groups) read every row, and the columns it needs come after the
-- recognised text, which SQLite steps over; these indexes hold them, so the
-- counts read the index alone (4 to 10 times faster).
DROP INDEX documents_drawer;
CREATE INDEX documents_drawer ON documents (workspace_id, drawer_id, deleted_at, private, person_id, added_by, uploader_access);
CREATE INDEX documents_cover ON documents (workspace_id, deleted_at, private, person_id, added_by, uploader_access,
    document_type_id, correspondent_id, location_id, created_date, inbox, drawer_id);
