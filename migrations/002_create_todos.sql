-- Migration: 002_create_todos
-- Creates the todos table
--
-- REFERENCES users(id) ON DELETE CASCADE means:
--   if a user is deleted, all their todos are automatically deleted too.
--   No orphaned todos left behind.

CREATE TABLE IF NOT EXISTS todos (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       TEXT        NOT NULL CHECK (char_length(title) <= 255),
    description TEXT        NOT NULL DEFAULT '',
    priority    TEXT        NOT NULL DEFAULT 'medium'
                            CHECK (priority IN ('low', 'medium', 'high')),
    done        BOOLEAN     NOT NULL DEFAULT FALSE,
    done_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index on user_id because every todo list query does: WHERE user_id = $1
CREATE INDEX IF NOT EXISTS idx_todos_user_id ON todos(user_id);

-- Index on done so we can efficiently filter: WHERE user_id = $1 AND done = false
CREATE INDEX IF NOT EXISTS idx_todos_user_done ON todos(user_id, done);

-- Reuse the same trigger function from migration 001
CREATE OR REPLACE TRIGGER todos_updated_at
    BEFORE UPDATE ON todos
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();