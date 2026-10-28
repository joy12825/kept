-- v1.5.7 (after paperless-ngx's mail rules): filter by recipient (an alias
-- such as bills+dad@…) and by words in the body, skip messages older than
-- max_age_days (0: no limit), and title attachments by the subject.
ALTER TABLE mail_accounts ADD COLUMN filter_to TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE mail_accounts ADD COLUMN filter_body TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE mail_accounts ADD COLUMN max_age_days BIGINT NOT NULL DEFAULT 0;
ALTER TABLE mail_accounts ADD COLUMN title_from TEXT COLLATE "C" NOT NULL DEFAULT 'filename' CHECK (title_from IN ('filename', 'subject'));

-- The log of handled messages: who sent it, the subject, what was done.
-- status is taken or skipped ('' for rows from before v1.5.7, not listed);
-- action is what was done to the message on the server (read, move, or ''
-- when it was left alone). Deleting a row has the message fetched again.
ALTER TABLE mail_seen ADD COLUMN sender TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE mail_seen ADD COLUMN subject TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE mail_seen ADD COLUMN sent_at BIGINT;
ALTER TABLE mail_seen ADD COLUMN status TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE mail_seen ADD COLUMN reason TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE mail_seen ADD COLUMN taken BIGINT NOT NULL DEFAULT 0;
ALTER TABLE mail_seen ADD COLUMN action TEXT COLLATE "C" NOT NULL DEFAULT '';
CREATE INDEX mail_seen_recent ON mail_seen (account_id, seen_at);
