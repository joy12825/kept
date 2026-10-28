-- Email is the second login identifier: unique, stored lower-cased.
UPDATE users SET email = NULL WHERE trim(email) = '';
UPDATE users SET email = lower(trim(email)) WHERE email IS NOT NULL;
CREATE UNIQUE INDEX users_email ON users (email) WHERE email IS NOT NULL;
