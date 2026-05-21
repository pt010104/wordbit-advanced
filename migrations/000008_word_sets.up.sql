-- Per-user word sets. Each user has at least one "Default" set, automatically
-- created here for every existing user. All existing user_word_states are
-- backfilled to the user's default set.
--
-- A set has one of two modes:
--   - "new_words": the system can auto-generate new words into this set
--                  (only one set per user may carry this mode at a time)
--   - "custom":    no auto-generation; user manually adds words; SRS still applies
--
-- The active set is tracked on user_settings.active_word_set_id and is
-- referenced by daily learning pools to scope due/new word selection.

CREATE TABLE word_sets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    icon TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT 'custom' CHECK (mode IN ('new_words', 'custom')),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_word_sets_user ON word_sets (user_id);
CREATE UNIQUE INDEX idx_word_sets_user_default
    ON word_sets (user_id) WHERE is_default = TRUE;
CREATE UNIQUE INDEX idx_word_sets_user_new_words_mode
    ON word_sets (user_id) WHERE mode = 'new_words';
CREATE UNIQUE INDEX idx_word_sets_user_name_lower
    ON word_sets (user_id, lower(name));

CREATE TRIGGER trg_word_sets_updated_at BEFORE UPDATE ON word_sets
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Backfill: create a "Default" set per existing user, with mode=new_words so
-- the historical auto-generation behavior is preserved for current users.
INSERT INTO word_sets (user_id, name, icon, mode, is_default)
SELECT u.id, 'Default', 'default', 'new_words', TRUE
FROM users u
ON CONFLICT DO NOTHING;

ALTER TABLE user_word_states
    ADD COLUMN word_set_id UUID REFERENCES word_sets(id) ON DELETE SET NULL;

-- Existing per-user word states default to the user's default set.
UPDATE user_word_states uws
SET word_set_id = ws.id
FROM word_sets ws
WHERE ws.user_id = uws.user_id AND ws.is_default = TRUE AND uws.word_set_id IS NULL;

CREATE INDEX idx_user_word_states_set ON user_word_states (user_id, word_set_id);

-- When the learning service inserts a brand-new user_word_state (first
-- exposure to a generated new word, or dictionary-created entry that hasn't
-- been explicitly assigned), default its word_set_id to the user's
-- new_words-mode set. The pool generator always targets the new_words set, so
-- this keeps all generated new words owned by that set regardless of which
-- set the user is currently viewing. Dictionary creates that explicitly pass
-- a word_set_id will skip this default (the value is already non-null).
CREATE OR REPLACE FUNCTION assign_default_word_set_for_user_word_state()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.word_set_id IS NULL THEN
        SELECT id INTO NEW.word_set_id
        FROM word_sets
        WHERE user_id = NEW.user_id AND mode = 'new_words'
        LIMIT 1;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_user_word_states_default_set
BEFORE INSERT ON user_word_states
FOR EACH ROW EXECUTE FUNCTION assign_default_word_set_for_user_word_state();

ALTER TABLE user_settings
    ADD COLUMN active_word_set_id UUID REFERENCES word_sets(id) ON DELETE SET NULL;

UPDATE user_settings us
SET active_word_set_id = ws.id
FROM word_sets ws
WHERE ws.user_id = us.user_id AND ws.is_default = TRUE AND us.active_word_set_id IS NULL;

ALTER TABLE daily_learning_pools
    ADD COLUMN word_set_id UUID REFERENCES word_sets(id) ON DELETE SET NULL;

CREATE INDEX idx_daily_learning_pools_user_date_set
    ON daily_learning_pools (user_id, local_date DESC, word_set_id);
