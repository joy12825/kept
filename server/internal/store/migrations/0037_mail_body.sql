-- v1.4.0: save the message itself as a document (bills that come only in
-- the body). none: attachments only (what accounts did before);
-- no_attachments: the body when a message has no attachment we take;
-- always: the body as well as the attachments.
ALTER TABLE mail_accounts ADD COLUMN body_mode TEXT NOT NULL DEFAULT 'none' CHECK (body_mode IN ('none', 'no_attachments', 'always'));
