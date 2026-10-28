-- v1.3.7: when a person last edited the recognised text (ms); null when
-- it is as recognised. Reprocessing replaces the text and clears it.
ALTER TABLE documents ADD COLUMN content_edited_at BIGINT;
