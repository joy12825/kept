-- Milestone 1.7: the scheduler and the classifier.
--
-- schedules holds one row per scheduler entry (jobs.Scheduler): when it is
-- due next and when it last enqueued its job.
CREATE TABLE schedules (
    name        TEXT    PRIMARY KEY,
    next_run_at INTEGER NOT NULL,
    last_run_at INTEGER
);

-- One trained classifier per workspace (design doc 3.5, 5.6). A model whose
-- format_version differs from the code's is treated as missing. data_hash
-- fingerprints the training data so an unchanged workspace is not retrained.
-- summary is JSON for the settings page (samples per object, skip reason).
CREATE TABLE classifier_models (
    workspace_id   INTEGER PRIMARY KEY REFERENCES workspaces (id) ON DELETE CASCADE,
    format_version INTEGER NOT NULL,
    trained_at     INTEGER NOT NULL,
    data_hash      TEXT    NOT NULL,
    doc_count      INTEGER NOT NULL,
    summary        TEXT    NOT NULL DEFAULT '{}',
    blob           BLOB
);
