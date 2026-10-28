-- v1.6.4: sensitive documents (an identity card, the household register, a
-- bank card): a document, or every document in a drawer, can be marked so.
-- Their preview, files and recognised text need the viewer to have
-- confirmed the password (or a two-factor code) recently: sessions keep
-- when that was. Signing in counts as confirming.
ALTER TABLE documents ADD COLUMN sensitive INTEGER NOT NULL DEFAULT 0;
ALTER TABLE drawers ADD COLUMN sensitive INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN verified_at INTEGER NOT NULL DEFAULT 0;
