CREATE TABLE context_exercise_packs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    local_date DATE NOT NULL,
    topic TEXT NOT NULL,
    cefr_level TEXT NOT NULL CHECK (cefr_level IN ('B1', 'B2', 'C1', 'C2')),
    pack_type TEXT NOT NULL CHECK (pack_type IN ('context_cluster_challenge')),
    cluster_hash TEXT NOT NULL,
    source_words_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL CHECK (status IN ('ready')),
    llm_run_id UUID REFERENCES llm_generation_runs(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT context_exercise_packs_user_date_cluster_unique UNIQUE (user_id, local_date, cluster_hash, pack_type)
);

CREATE INDEX idx_context_exercise_packs_user_date_created
    ON context_exercise_packs (user_id, local_date DESC, created_at DESC);

CREATE TRIGGER trg_context_exercise_packs_updated_at BEFORE UPDATE ON context_exercise_packs
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
