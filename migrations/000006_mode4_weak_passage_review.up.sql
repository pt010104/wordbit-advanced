CREATE TABLE mode4_review_passages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    generation_number INTEGER NOT NULL CHECK (generation_number > 0),
    word_ids_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    source_words_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    plain_passage_text TEXT NOT NULL DEFAULT '',
    marked_passage_markdown TEXT NOT NULL DEFAULT '',
    passage_spans_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    status TEXT NOT NULL CHECK (status IN ('active', 'completed', 'superseded')),
    skip_count INTEGER NOT NULL DEFAULT 0 CHECK (skip_count >= 0),
    last_skipped_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    llm_run_id UUID REFERENCES llm_generation_runs(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT mode4_review_passages_user_generation_unique UNIQUE (user_id, generation_number)
);

CREATE UNIQUE INDEX idx_mode4_review_passages_user_active
    ON mode4_review_passages (user_id)
    WHERE status = 'active';

CREATE INDEX idx_mode4_review_passages_user_created
    ON mode4_review_passages (user_id, created_at DESC);

CREATE TABLE mode4_review_states (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    generation_count INTEGER NOT NULL DEFAULT 0 CHECK (generation_count >= 0),
    active_passage_id UUID REFERENCES mode4_review_passages(id) ON DELETE SET NULL,
    last_completed_at TIMESTAMPTZ,
    next_eligible_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_mode4_review_states_next_eligible
    ON mode4_review_states (next_eligible_at);

CREATE TRIGGER trg_mode4_review_passages_updated_at BEFORE UPDATE ON mode4_review_passages
FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_mode4_review_states_updated_at BEFORE UPDATE ON mode4_review_states
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
