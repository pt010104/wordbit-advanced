CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    external_subject TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE user_settings (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    cefr_level TEXT NOT NULL DEFAULT 'B1' CHECK (cefr_level IN ('B1', 'B2', 'C1', 'C2')),
    daily_new_word_limit INTEGER NOT NULL DEFAULT 10 CHECK (daily_new_word_limit >= 0 AND daily_new_word_limit <= 50),
    preferred_meaning_language TEXT NOT NULL DEFAULT 'vi' CHECK (preferred_meaning_language IN ('vi', 'en')),
    timezone TEXT NOT NULL DEFAULT 'Asia/Ho_Chi_Minh',
    pronunciation_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    lock_screen_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE words (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    word TEXT NOT NULL,
    normalized_form TEXT NOT NULL,
    canonical_form TEXT NOT NULL DEFAULT '',
    lemma TEXT NOT NULL DEFAULT '',
    word_family TEXT NOT NULL DEFAULT '',
    confusable_group_key TEXT NOT NULL DEFAULT '',
    part_of_speech TEXT NOT NULL DEFAULT '',
    level TEXT NOT NULL CHECK (level IN ('B1', 'B2', 'C1', 'C2')),
    topic TEXT NOT NULL,
    ipa TEXT NOT NULL DEFAULT '',
    pronunciation_hint TEXT NOT NULL DEFAULT '',
    vietnamese_meaning TEXT NOT NULL,
    english_meaning TEXT NOT NULL,
    example_sentence_1 TEXT NOT NULL DEFAULT '',
    example_sentence_2 TEXT NOT NULL DEFAULT '',
    source_provider TEXT NOT NULL DEFAULT '',
    source_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT words_normalized_pos_unique UNIQUE (normalized_form, part_of_speech)
);

CREATE INDEX idx_words_level_topic ON words (level, topic);
CREATE INDEX idx_words_confusable ON words (confusable_group_key);
CREATE INDEX idx_words_lemma ON words (lemma);

CREATE TABLE user_word_states (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    word_id UUID NOT NULL REFERENCES words(id) ON DELETE CASCADE,
    status TEXT NOT NULL CHECK (status IN ('known', 'learning', 'review')),
    first_seen_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ,
    last_rating TEXT NOT NULL DEFAULT '' CHECK (last_rating IN ('', 'easy', 'medium', 'hard')),
    next_review_at TIMESTAMPTZ,
    interval_seconds INTEGER NOT NULL DEFAULT 0 CHECK (interval_seconds >= 0),
    stability DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    difficulty DOUBLE PRECISION NOT NULL DEFAULT 0.5,
    review_count INTEGER NOT NULL DEFAULT 0,
    wrong_count INTEGER NOT NULL DEFAULT 0,
    easy_count INTEGER NOT NULL DEFAULT 0,
    medium_count INTEGER NOT NULL DEFAULT 0,
    hard_count INTEGER NOT NULL DEFAULT 0,
    hint_used_count INTEGER NOT NULL DEFAULT 0,
    reveal_meaning_count INTEGER NOT NULL DEFAULT 0,
    reveal_example_count INTEGER NOT NULL DEFAULT 0,
    avg_response_time_ms BIGINT NOT NULL DEFAULT 0,
    weakness_score DOUBLE PRECISION NOT NULL DEFAULT 0,
    learning_stage INTEGER NOT NULL DEFAULT 0 CHECK (learning_stage >= 0 AND learning_stage <= 3),
    last_mode TEXT NOT NULL DEFAULT '' CHECK (last_mode IN ('', 'hidden_meaning', 'multiple_choice', 'fill_in_blank')),
    known_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, word_id)
);

CREATE INDEX idx_user_word_states_next_review ON user_word_states (user_id, next_review_at);
CREATE INDEX idx_user_word_states_status ON user_word_states (user_id, status);
CREATE INDEX idx_user_word_states_weakness ON user_word_states (user_id, weakness_score DESC);

CREATE TABLE daily_learning_pools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    local_date DATE NOT NULL,
    timezone TEXT NOT NULL,
    topic TEXT NOT NULL,
    due_review_count INTEGER NOT NULL DEFAULT 0,
    short_term_count INTEGER NOT NULL DEFAULT 0,
    weak_count INTEGER NOT NULL DEFAULT 0,
    new_count INTEGER NOT NULL DEFAULT 0,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT daily_learning_pools_user_date_unique UNIQUE (user_id, local_date)
);

CREATE INDEX idx_daily_learning_pools_user_date ON daily_learning_pools (user_id, local_date DESC);

CREATE TABLE daily_learning_pool_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pool_id UUID NOT NULL REFERENCES daily_learning_pools(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    word_id UUID NOT NULL REFERENCES words(id) ON DELETE CASCADE,
    ordinal INTEGER NOT NULL CHECK (ordinal > 0),
    item_type TEXT NOT NULL CHECK (item_type IN ('review', 'short_term', 'weak', 'new')),
    review_mode TEXT NOT NULL CHECK (review_mode IN ('hidden_meaning', 'multiple_choice', 'fill_in_blank')),
    due_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed')),
    is_review BOOLEAN NOT NULL DEFAULT FALSE,
    first_exposure_required BOOLEAN NOT NULL DEFAULT FALSE,
    revealed_meaning BOOLEAN NOT NULL DEFAULT FALSE,
    revealed_example BOOLEAN NOT NULL DEFAULT FALSE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT daily_learning_pool_items_pool_word_unique UNIQUE (pool_id, word_id, item_type, ordinal)
);

CREATE INDEX idx_daily_learning_pool_items_pending ON daily_learning_pool_items (user_id, status, due_at);
CREATE INDEX idx_daily_learning_pool_items_pool_ordinal ON daily_learning_pool_items (pool_id, ordinal);

CREATE TABLE learning_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    word_id UUID NOT NULL REFERENCES words(id) ON DELETE CASCADE,
    pool_item_id UUID REFERENCES daily_learning_pool_items(id) ON DELETE SET NULL,
    event_type TEXT NOT NULL,
    event_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    response_time_ms INTEGER NOT NULL DEFAULT 0,
    mode_used TEXT NOT NULL DEFAULT '' CHECK (mode_used IN ('', 'hidden_meaning', 'multiple_choice', 'fill_in_blank')),
    client_event_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_learning_events_client_event
    ON learning_events (user_id, client_event_id)
    WHERE client_event_id <> '';

CREATE INDEX idx_learning_events_user_word_time ON learning_events (user_id, word_id, event_time DESC);

CREATE TABLE llm_generation_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    pool_id UUID REFERENCES daily_learning_pools(id) ON DELETE SET NULL,
    local_date DATE NOT NULL,
    topic TEXT NOT NULL,
    requested_count INTEGER NOT NULL,
    accepted_count INTEGER NOT NULL DEFAULT 0,
    attempt INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL CHECK (status IN ('success', 'failed', 'partial')),
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt TEXT NOT NULL,
    raw_response JSONB NOT NULL DEFAULT '{}'::jsonb,
    rejection_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_llm_generation_runs_user_date ON llm_generation_runs (user_id, local_date DESC, created_at DESC);

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_users_updated_at BEFORE UPDATE ON users
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_user_settings_updated_at BEFORE UPDATE ON user_settings
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_words_updated_at BEFORE UPDATE ON words
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_user_word_states_updated_at BEFORE UPDATE ON user_word_states
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_daily_learning_pools_updated_at BEFORE UPDATE ON daily_learning_pools
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_daily_learning_pool_items_updated_at BEFORE UPDATE ON daily_learning_pool_items
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_llm_generation_runs_updated_at BEFORE UPDATE ON llm_generation_runs
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
