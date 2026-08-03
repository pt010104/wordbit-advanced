CREATE TABLE user_word_recordings (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    word_id UUID NOT NULL REFERENCES words(id) ON DELETE CASCADE,
    object_key TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, word_id)
);

CREATE TRIGGER trg_user_word_recordings_updated_at BEFORE UPDATE ON user_word_recordings
FOR EACH ROW EXECUTE FUNCTION set_updated_at();
