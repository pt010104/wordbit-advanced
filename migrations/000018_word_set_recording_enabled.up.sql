-- Do not amend already-applied migrations. Existing installations may have
-- version 13 recorded before voice reading was introduced.
ALTER TABLE word_sets
    ADD COLUMN IF NOT EXISTS recording_enabled BOOLEAN NOT NULL DEFAULT FALSE;
