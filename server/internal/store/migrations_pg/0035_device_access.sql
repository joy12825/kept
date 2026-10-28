-- App 2.0: what a paired phone may do. upload (App 1.x): only upload;
-- full: the user's role in the workspace, short of account security and
-- administration (api.deviceDenied).
ALTER TABLE api_tokens ADD COLUMN access TEXT COLLATE "C" NOT NULL DEFAULT 'upload';
