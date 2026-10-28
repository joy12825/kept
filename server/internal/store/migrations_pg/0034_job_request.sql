-- v1.0.1: the ID of the HTTP request that enqueued a job ('' for scheduled
-- jobs), so a job's logs can be matched with the request's.
ALTER TABLE jobs ADD COLUMN request_id TEXT COLLATE "C" NOT NULL DEFAULT '';
