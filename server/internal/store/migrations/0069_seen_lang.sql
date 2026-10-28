-- v1.10.2: the language the person last used the web or the app in, for
-- the emails sent to them when their account follows the browser
-- (users.locale ''): 'zh', 'en' or '' (not seen yet).
ALTER TABLE users ADD COLUMN seen_lang TEXT NOT NULL DEFAULT '';
