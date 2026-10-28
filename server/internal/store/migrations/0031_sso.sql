-- v1.0 stage 35: single sign-on. An account is bound to one person at the
-- OpenID Connect provider (issuer + subject) the first time they sign in
-- there with the account's email.
ALTER TABLE users ADD COLUMN sso_issuer TEXT;
ALTER TABLE users ADD COLUMN sso_subject TEXT;
CREATE UNIQUE INDEX users_sso ON users (sso_issuer, sso_subject) WHERE sso_subject IS NOT NULL;
