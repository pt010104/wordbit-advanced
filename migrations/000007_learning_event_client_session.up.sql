ALTER TABLE learning_events
    ADD COLUMN client_session_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_learning_events_user_session_time
    ON learning_events (user_id, client_session_id, event_time DESC)
    WHERE client_session_id <> '';
