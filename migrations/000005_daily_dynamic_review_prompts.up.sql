CREATE TABLE daily_dynamic_review_prompts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    local_date DATE NOT NULL,
    word_id UUID NOT NULL REFERENCES words(id) ON DELETE CASCADE,
    review_mode TEXT NOT NULL CHECK (review_mode IN ('multiple_choice', 'fill_in_blank')),
    payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    llm_run_id UUID REFERENCES llm_generation_runs(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT daily_dynamic_review_prompts_user_date_word_mode_unique UNIQUE (user_id, local_date, word_id, review_mode)
);

CREATE INDEX idx_daily_dynamic_review_prompts_user_date
    ON daily_dynamic_review_prompts (user_id, local_date DESC, created_at DESC);

CREATE TRIGGER trg_daily_dynamic_review_prompts_updated_at BEFORE UPDATE ON daily_dynamic_review_prompts
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
