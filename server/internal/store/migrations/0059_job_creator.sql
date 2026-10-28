-- v1.7.14: who started a job, so the task list can show “mine”. NULL for
-- scheduled jobs and anything without a person behind it. Uploads before
-- this take it from the upload's recorded actor.
ALTER TABLE jobs ADD COLUMN created_by INTEGER REFERENCES users (id) ON DELETE SET NULL;
UPDATE jobs SET created_by = (SELECT u.id FROM users AS u WHERE u.id = CAST(json_extract(jobs.payload, '$.actor.UserID') AS INTEGER))
WHERE kind = 'ingest.document';
