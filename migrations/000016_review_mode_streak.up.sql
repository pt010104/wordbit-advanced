-- The per-word streak lets custom sets rotate away from their hardest enabled
-- mode after three consecutive cards, without changing completed history.
ALTER TABLE user_word_states
    ADD COLUMN mode_streak_count INTEGER NOT NULL DEFAULT 0
    CHECK (mode_streak_count >= 0);
