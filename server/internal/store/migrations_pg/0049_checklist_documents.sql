-- v1.6.7: documents put into a checklist item by hand (uploaded from the
-- checklist, or chosen there): they count for the item whatever its filter
-- says, before the documents the filter finds.
CREATE TABLE checklist_documents (
    checklist_id BIGINT NOT NULL REFERENCES checklists (id) ON DELETE CASCADE,
    item_id      TEXT COLLATE "C" NOT NULL,
    document_id  BIGINT NOT NULL REFERENCES documents (id) ON DELETE CASCADE,
    added_at     BIGINT NOT NULL,
    PRIMARY KEY (checklist_id, item_id, document_id)
);
CREATE INDEX checklist_documents_document ON checklist_documents (document_id);
