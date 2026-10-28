-- v1.5.1: which attachments to take, by their file names (keywords, any of
-- them; '' takes all, or excludes none), and the correspondent taken from
-- who sent the message (matched by name, created when missing).
ALTER TABLE mail_accounts ADD COLUMN attach_include TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE mail_accounts ADD COLUMN attach_exclude TEXT COLLATE "C" NOT NULL DEFAULT '';
ALTER TABLE mail_accounts ADD COLUMN correspondent_from_sender BIGINT NOT NULL DEFAULT 0;
