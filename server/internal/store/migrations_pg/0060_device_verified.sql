-- v1.7.20: a paired phone (a full-access device token) can confirm the
-- password or a two-factor code too, like a web session (v1.6.4), and then
-- open sensitive documents for 15 minutes. 0: never confirmed.
ALTER TABLE api_tokens ADD COLUMN verified_at BIGINT NOT NULL DEFAULT 0;
