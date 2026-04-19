DROP INDEX IF EXISTS idx_learning_events_user_session_time;

ALTER TABLE learning_events
    DROP COLUMN IF EXISTS client_session_id;
