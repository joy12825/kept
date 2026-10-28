-- v0.5 stage 30: the question-answering agent's conversations, private
-- to the person who asked.
CREATE TABLE qa_conversations (
    id           INTEGER PRIMARY KEY,
    workspace_id INTEGER NOT NULL REFERENCES workspaces (id) ON DELETE CASCADE,
    user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title        TEXT NOT NULL,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX qa_conversations_user ON qa_conversations (user_id, workspace_id, updated_at);

CREATE TABLE qa_messages (
    id              INTEGER PRIMARY KEY,
    conversation_id INTEGER NOT NULL REFERENCES qa_conversations (id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content         TEXT NOT NULL,
    sources         TEXT NOT NULL DEFAULT '[]',
    status          TEXT NOT NULL DEFAULT 'done' CHECK (status IN ('done', 'stopped', 'failed')),
    run_id          INTEGER REFERENCES agent_runs (id) ON DELETE SET NULL,
    created_at      INTEGER NOT NULL
);
CREATE INDEX qa_messages_conversation ON qa_messages (conversation_id, id);
