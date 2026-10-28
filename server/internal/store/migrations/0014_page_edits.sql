-- v0.2: PDF page editing and password-protected uploads.
--
-- page_edits: the requested rotations and deleted pages (JSON, pages of the
-- unedited PDF) and whether the unedited PDF is the archive (kept beside it
-- as <id>.base.pdf) or the original. '' means not edited.
-- original_encrypted: the original is a password-protected PDF; its
-- decrypted copy is the archive and reprocessing reads that instead.
ALTER TABLE documents ADD COLUMN page_edits TEXT NOT NULL DEFAULT '';
ALTER TABLE documents ADD COLUMN original_encrypted INTEGER NOT NULL DEFAULT 0;
