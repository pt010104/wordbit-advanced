DROP INDEX IF EXISTS idx_daily_learning_pools_user_date_set;
ALTER TABLE daily_learning_pools DROP COLUMN IF EXISTS word_set_id;

ALTER TABLE user_settings DROP COLUMN IF EXISTS active_word_set_id;

DROP TRIGGER IF EXISTS trg_user_word_states_default_set ON user_word_states;
DROP FUNCTION IF EXISTS assign_default_word_set_for_user_word_state();
DROP INDEX IF EXISTS idx_user_word_states_set;
ALTER TABLE user_word_states DROP COLUMN IF EXISTS word_set_id;

DROP TRIGGER IF EXISTS trg_word_sets_updated_at ON word_sets;
DROP INDEX IF EXISTS idx_word_sets_user_name_lower;
DROP INDEX IF EXISTS idx_word_sets_user_new_words_mode;
DROP INDEX IF EXISTS idx_word_sets_user_default;
DROP INDEX IF EXISTS idx_word_sets_user;
DROP TABLE IF EXISTS word_sets;
