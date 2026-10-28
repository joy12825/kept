-- v1.10.0: the language of the web for an account: '' follows the browser,
-- 'zh' or 'en'. The server answers in it from v1.10.1.
ALTER TABLE users ADD COLUMN locale TEXT NOT NULL DEFAULT '';
